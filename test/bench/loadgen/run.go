package main

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"
)

// sample is the outcome of one request.
type sample struct {
	scheduled time.Time     // when the request was due (open loop) or started (closed loop)
	latency   time.Duration // from scheduled to the end of the response body
	done      time.Time
	ok        bool
}

// config is what a run needs.
type config struct {
	URL      string
	Rate     float64       // requests per second; 0 means closed loop
	Conns    int           // workers: connections in use, at most
	Duration time.Duration // measured window
	Warmup   time.Duration // run first and discarded
	Timeout  time.Duration // per request
}

func newClient(conns int, timeout time.Duration) *http.Client {
	tr := &http.Transport{
		MaxIdleConns:        conns,
		MaxIdleConnsPerHost: conns,
		MaxConnsPerHost:     conns,
		IdleConnTimeout:     time.Minute,
	}
	return &http.Client{Transport: tr, Timeout: timeout}
}

// do sends one GET and reads the whole body.
func do(client *http.Client, url string) bool {
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}

// runOpen sends requests on a fixed schedule. Latency is counted from the time each request was due, not from
// when a worker picked it up, so a server that falls behind shows its queue instead of hiding it.
func runOpen(ctx context.Context, cfg config) (samples []sample, windowStart time.Time, sent int) {
	client := newClient(cfg.Conns, cfg.Timeout)
	defer client.CloseIdleConnections()

	total := requestCount(cfg.Rate, cfg.Warmup, cfg.Duration)
	due := make(chan time.Time, total)
	out := make(chan sample, total)

	var wg sync.WaitGroup
	for i := 0; i < cfg.Conns; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for t := range due {
				ok := do(client, cfg.URL)
				now := time.Now()
				out <- sample{scheduled: t, latency: now.Sub(t), done: now, ok: ok}
			}
		}()
	}

	start := time.Now()
	windowStart = start.Add(cfg.Warmup)
produce:
	for i := 0; i < total; i++ {
		t := scheduledAt(start, cfg.Rate, i)
		if wait := time.Until(t); wait > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				break produce
			}
		}
		due <- t
	}
	close(due)
	wg.Wait()
	close(out)

	for s := range out {
		sent++
		if !s.scheduled.Before(windowStart) {
			samples = append(samples, s)
		}
	}
	return samples, windowStart, sent
}

// runClosed keeps cfg.Conns requests in flight, each worker sending the next as soon as the last is answered.
// It finds how much the server can do, but a slow server slows the load down with it.
func runClosed(ctx context.Context, cfg config) (samples []sample, windowStart time.Time, sent int) {
	client := newClient(cfg.Conns, cfg.Timeout)
	defer client.CloseIdleConnections()

	start := time.Now()
	windowStart = start.Add(cfg.Warmup)
	end := windowStart.Add(cfg.Duration)

	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i < cfg.Conns; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for ctx.Err() == nil {
				t := time.Now()
				if !t.Before(end) {
					return
				}
				ok := do(client, cfg.URL)
				now := time.Now()
				mu.Lock()
				sent++
				if !t.Before(windowStart) {
					samples = append(samples, sample{scheduled: t, latency: now.Sub(t), done: now, ok: ok})
				}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return samples, windowStart, sent
}

// measure runs the load and turns the samples into a result.
func measure(ctx context.Context, cfg config) result {
	var samples []sample
	var windowStart time.Time
	res := result{Connections: cfg.Conns, WarmupS: cfg.Warmup.Seconds()}
	if cfg.Rate > 0 {
		res.Mode = "open"
		res.OfferedRate = cfg.Rate
		res.LatencyOrigin = "scheduled time"
		samples, windowStart, res.Sent = runOpen(ctx, cfg)
	} else {
		res.Mode = "closed"
		res.LatencyOrigin = "request start"
		samples, windowStart, res.Sent = runClosed(ctx, cfg)
	}

	var latencies []time.Duration
	var last time.Time
	for _, s := range samples {
		if !s.ok {
			res.Errors++
			continue
		}
		res.Requests++
		latencies = append(latencies, s.latency)
		if s.done.After(last) {
			last = s.done
		}
	}
	mean, median, p99, max := summarize(latencies)
	res.MeanMs, res.MedianMs, res.P99Ms, res.MaxMs = ms(mean), ms(median), ms(p99), ms(max)

	// The rate is counted over the measured window; if the last answer came after the window ended, the window is
	// stretched to it, so a server that fell behind does not look faster than it was.
	window := cfg.Duration
	if elapsed := last.Sub(windowStart); elapsed > window {
		window = elapsed
	}
	res.DurationS = window.Seconds()
	if window > 0 {
		res.AchievedRate = float64(res.Requests) / window.Seconds()
	}
	return res
}
