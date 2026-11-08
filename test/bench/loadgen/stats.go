package main

import (
	"math"
	"sort"
	"time"
)

// result is what one load run reports. It is printed as JSON, so the benchmark script can read it.
type result struct {
	Mode          string  `json:"mode"`          // "open" (fixed rate) or "closed" (fixed connections)
	OfferedRate   float64 `json:"offered_rate"`  // requests per second asked for; 0 in closed mode
	AchievedRate  float64 `json:"achieved_rate"` // requests per second that were answered
	Requests      int     `json:"requests"`      // requests answered with a 2xx status, inside the measured window
	Errors        int     `json:"errors"`        // requests that failed or got another status, inside the measured window
	Sent          int     `json:"sent"`          // every request that was sent and answered or failed, warm-up included
	DurationS     float64 `json:"duration_s"`    // length of the measured window
	MeanMs        float64 `json:"mean_ms"`
	MedianMs      float64 `json:"median_ms"`
	P99Ms         float64 `json:"p99_ms"`
	MaxMs         float64 `json:"max_ms"`
	Connections   int     `json:"connections"`
	WarmupS       float64 `json:"warmup_s"`
	LatencyOrigin string  `json:"latency_origin"` // where latency is counted from
}

// percentile returns the p-th percentile (0 to 100) of sorted, by the nearest-rank method: the smallest value
// that is at least p percent of the data. It returns 0 for no data.
func percentile(sorted []time.Duration, p float64) time.Duration {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if p <= 0 {
		return sorted[0]
	}
	rank := int(math.Ceil(p / 100 * float64(n)))
	if rank < 1 {
		rank = 1
	}
	if rank > n {
		rank = n
	}
	return sorted[rank-1]
}

// summarize turns the latencies of the measured requests into the figures of a result. It sorts a copy.
func summarize(latencies []time.Duration) (mean, median, p99, max time.Duration) {
	if len(latencies) == 0 {
		return 0, 0, 0, 0
	}
	sorted := append([]time.Duration(nil), latencies...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	var sum time.Duration
	for _, l := range sorted {
		sum += l
	}
	mean = sum / time.Duration(len(sorted))
	return mean, percentile(sorted, 50), percentile(sorted, 99), sorted[len(sorted)-1]
}

// scheduledAt is when request number i (counting from 0) is due, for a fixed rate in requests per second. The
// schedule does not depend on how fast earlier requests were answered.
func scheduledAt(start time.Time, rate float64, i int) time.Time {
	return start.Add(time.Duration(float64(i) * float64(time.Second) / rate))
}

// requestCount is how many requests a run schedules for the warm-up and the measured window together.
func requestCount(rate float64, warmup, duration time.Duration) int {
	return int(math.Round(rate * (warmup + duration).Seconds()))
}

// ms converts a duration to fractional milliseconds.
func ms(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
