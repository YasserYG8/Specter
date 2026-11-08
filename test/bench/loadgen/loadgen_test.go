package main

import (
	"context"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func ds(ms ...int) []time.Duration {
	out := make([]time.Duration, len(ms))
	for i, m := range ms {
		out[i] = time.Duration(m) * time.Millisecond
	}
	return out
}

func TestPercentile(t *testing.T) {
	cases := []struct {
		name   string
		sorted []time.Duration
		p      float64
		want   time.Duration
	}{
		{"no data", nil, 50, 0},
		{"one value, median", ds(7), 50, 7 * time.Millisecond},
		{"one value, p99", ds(7), 99, 7 * time.Millisecond},
		{"odd count median", ds(1, 2, 3, 4, 5), 50, 3 * time.Millisecond},
		{"even count median is the lower middle", ds(1, 2, 3, 4), 50, 2 * time.Millisecond},
		{"p99 of 100 values is the 99th", func() []time.Duration {
			s := make([]time.Duration, 100)
			for i := range s {
				s[i] = time.Duration(i+1) * time.Millisecond
			}
			return s
		}(), 99, 99 * time.Millisecond},
		{"p99 of 10 values is the largest", ds(1, 2, 3, 4, 5, 6, 7, 8, 9, 10), 99, 10 * time.Millisecond},
		{"p100 is the largest", ds(1, 5, 9), 100, 9 * time.Millisecond},
		{"p0 is the smallest", ds(1, 5, 9), 0, 1 * time.Millisecond},
		{"p above 100 does not run off the end", ds(1, 5, 9), 250, 9 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := percentile(tc.sorted, tc.p); got != tc.want {
				t.Errorf("percentile(%v, %v) = %v, want %v", tc.sorted, tc.p, got, tc.want)
			}
		})
	}
}

func TestSummarizeDoesNotNeedSortedInputAndDoesNotChangeIt(t *testing.T) {
	in := ds(9, 1, 5, 3, 7)
	mean, median, p99, max := summarize(in)
	if median != 5*time.Millisecond || p99 != 9*time.Millisecond || max != 9*time.Millisecond || mean != 5*time.Millisecond {
		t.Errorf("mean %v median %v p99 %v max %v", mean, median, p99, max)
	}
	if in[0] != 9*time.Millisecond || in[1] != 1*time.Millisecond {
		t.Errorf("summarize reordered its input: %v", in)
	}
	if m, md, p, x := summarize(nil); m != 0 || md != 0 || p != 0 || x != 0 {
		t.Error("no data should give zeros")
	}
}

func TestScheduledAtDoesNotDependOnEarlierRequests(t *testing.T) {
	start := time.Unix(1000, 0)
	if got := scheduledAt(start, 1000, 0); !got.Equal(start) {
		t.Errorf("request 0 is due at the start, got %v", got.Sub(start))
	}
	if got := scheduledAt(start, 1000, 1); got.Sub(start) != time.Millisecond {
		t.Errorf("at 1000/s, request 1 is due after 1ms, got %v", got.Sub(start))
	}
	if got := scheduledAt(start, 1000, 2500); got.Sub(start) != 2500*time.Millisecond {
		t.Errorf("at 1000/s, request 2500 is due after 2.5s, got %v", got.Sub(start))
	}
	if got := scheduledAt(start, 100, 3); got.Sub(start) != 30*time.Millisecond {
		t.Errorf("at 100/s, request 3 is due after 30ms, got %v", got.Sub(start))
	}
	// No drift: the schedule of request n is exact, not a sum of rounded steps
	if got := scheduledAt(start, 3, 3000000); got.Sub(start) != 1000000*time.Second {
		t.Errorf("a long schedule drifted: %v", got.Sub(start))
	}
}

func TestRequestCount(t *testing.T) {
	if got := requestCount(1000, 2*time.Second, 10*time.Second); got != 12000 {
		t.Errorf("1000/s for 12s should be 12000 requests, got %d", got)
	}
	if got := requestCount(100, 0, 500*time.Millisecond); got != 50 {
		t.Errorf("100/s for 0.5s should be 50 requests, got %d", got)
	}
}

func fastServer() (*httptest.Server, *atomic.Int64) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_, _ = w.Write([]byte("ok"))
	}))
	return srv, &hits
}

func TestOpenLoopKeepsTheOfferedRate(t *testing.T) {
	srv, hits := fastServer()
	defer srv.Close()

	res := measure(context.Background(), config{
		URL: srv.URL, Rate: 200, Conns: 16, Duration: time.Second, Warmup: 200 * time.Millisecond, Timeout: 2 * time.Second,
	})

	if res.Mode != "open" || res.OfferedRate != 200 {
		t.Errorf("mode %q offered %v", res.Mode, res.OfferedRate)
	}
	if res.Errors != 0 {
		t.Errorf("unexpected errors: %d", res.Errors)
	}
	// The measured window holds 200 requests; the warm-up ones are sent but not counted
	if res.Requests < 195 || res.Requests > 205 {
		t.Errorf("expected about 200 measured requests, got %d", res.Requests)
	}
	if int64(res.Sent) != hits.Load() {
		t.Errorf("sent is %d, but the server saw %d requests: the count must match what the server wrote", res.Sent, hits.Load())
	}
	if hits.Load() < 235 {
		t.Errorf("the warm-up requests should be sent too: the server saw %d, expected about 240", hits.Load())
	}
	if res.AchievedRate < 0.9*200 {
		t.Errorf("achieved rate %.1f is far below the offered 200", res.AchievedRate)
	}
	if res.MedianMs <= 0 || res.P99Ms < res.MedianMs || res.MaxMs < res.P99Ms {
		t.Errorf("latencies out of order: median %v p99 %v max %v", res.MedianMs, res.P99Ms, res.MaxMs)
	}
}

// The point of the open loop: when the server is slower than the load, the latency grows with the queue. A closed
// loop would report only the service time and hide it.
func TestOpenLoopCountsLatencyFromTheScheduledTime(t *testing.T) {
	const service = 40 * time.Millisecond
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(service)
		_, _ = w.Write([]byte("ok"))
	}))
	defer slow.Close()

	// One connection serves 25 requests per second, and 100 are offered
	open := measure(context.Background(), config{
		URL: slow.URL, Rate: 100, Conns: 1, Duration: time.Second, Timeout: 10 * time.Second,
	})
	if open.MedianMs < 5*ms(service) {
		t.Errorf("the open loop should show the queue: median %.0f ms with a %.0f ms service time", open.MedianMs, ms(service))
	}
	if open.AchievedRate > 40 {
		t.Errorf("the server can do about 25 requests per second, but the achieved rate is %.1f", open.AchievedRate)
	}

	closed := measure(context.Background(), config{
		URL: slow.URL, Rate: 0, Conns: 1, Duration: time.Second, Timeout: 10 * time.Second,
	})
	if closed.Mode != "closed" {
		t.Errorf("mode %q", closed.Mode)
	}
	if closed.MedianMs > 3*ms(service) {
		t.Errorf("the closed loop measures the service time: median %.0f ms with a %.0f ms service time", closed.MedianMs, ms(service))
	}
	if math.Abs(closed.AchievedRate-25) > 8 {
		t.Errorf("closed loop on one connection should reach about 25 requests per second, got %.1f", closed.AchievedRate)
	}
}

func TestClosedLoopFindsThePeak(t *testing.T) {
	srv, _ := fastServer()
	defer srv.Close()

	res := measure(context.Background(), config{
		URL: srv.URL, Rate: 0, Conns: 8, Duration: 500 * time.Millisecond, Warmup: 100 * time.Millisecond, Timeout: 2 * time.Second,
	})
	if res.Requests < 100 {
		t.Errorf("a local server should answer many requests in half a second, got %d", res.Requests)
	}
	if res.Errors != 0 {
		t.Errorf("unexpected errors: %d", res.Errors)
	}
}

func TestErrorsAreCountedAndNotMeasured(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()

	res := measure(context.Background(), config{
		URL: bad.URL, Rate: 100, Conns: 4, Duration: 300 * time.Millisecond, Timeout: time.Second,
	})
	if res.Requests != 0 || res.Errors == 0 {
		t.Errorf("a 500 is an error, not a request: requests %d errors %d", res.Requests, res.Errors)
	}
}

func TestStoppingEarlyEndsTheRun(t *testing.T) {
	srv, _ := fastServer()
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	started := time.Now()
	measure(ctx, config{URL: srv.URL, Rate: 100, Conns: 4, Duration: 30 * time.Second, Timeout: time.Second})
	if took := time.Since(started); took > 5*time.Second {
		t.Errorf("an interrupted run should stop soon, took %v", took)
	}
}
