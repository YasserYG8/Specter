// Command loadgen sends HTTP requests to a server and prints one JSON result: requests, achieved rate, and the
// median and 99th percentile latency. It is the load for scripts/bench-overhead.sh.
//
// Two modes:
//
//	-rate 1000   open loop: 1000 requests per second on a schedule, latency counted from the scheduled time
//	-rate 0      closed loop: -conns connections, each sending its next request when the last is answered
//
// It has no dependencies on purpose, so it builds anywhere Go does and needs no wrk or ab.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"time"
)

func main() {
	var cfg config
	flag.StringVar(&cfg.URL, "url", "http://127.0.0.1:8080/json", "URL to request")
	flag.Float64Var(&cfg.Rate, "rate", 0, "requests per second (open loop); 0 means closed loop")
	flag.IntVar(&cfg.Conns, "conns", 32, "connections (workers); in open loop, the most requests in flight")
	flag.DurationVar(&cfg.Duration, "duration", 10*time.Second, "length of the measured window")
	flag.DurationVar(&cfg.Warmup, "warmup", 2*time.Second, "time to run first and discard")
	flag.DurationVar(&cfg.Timeout, "timeout", 5*time.Second, "timeout of one request")
	flag.Parse()

	if cfg.Rate < 0 || cfg.Conns < 1 || cfg.Duration <= 0 || cfg.Warmup < 0 || cfg.Timeout <= 0 {
		fmt.Fprintln(os.Stderr, "loadgen: -rate and -warmup must not be negative; -conns, -duration and -timeout must be positive")
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	res := measure(ctx, cfg)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(res); err != nil {
		fmt.Fprintln(os.Stderr, "loadgen:", err)
		os.Exit(1)
	}
	if res.Requests == 0 {
		fmt.Fprintf(os.Stderr, "loadgen: no request to %s succeeded (%d failed)\n", cfg.URL, res.Errors)
		os.Exit(1)
	}
}
