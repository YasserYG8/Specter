//go:build linux

// ringbuf-spike loads the spike program and prints what it captures from one process. It is the manual way to
// look at the program; the proofs are in spike_test.go.
//
//	sudo ./ringbuf-spike -pid 4120 [-depth 1]
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
)

func main() {
	pid := flag.Int("pid", 0, "PID to follow, as this process sees it")
	depth := flag.Uint("depth", 1, "how deep below the target to follow children")
	flag.Parse()
	if *pid <= 0 {
		fmt.Fprintln(os.Stderr, "usage: ringbuf-spike -pid <pid> [-depth n]   (needs root)")
		os.Exit(2)
	}

	s, err := newSession(uint32(*depth), 0)
	if err != nil {
		fmt.Fprintln(os.Stderr, "ringbuf-spike:", err)
		os.Exit(1)
	}
	defer s.close()
	if err := s.follow(*pid, 0); err != nil {
		fmt.Fprintln(os.Stderr, "ringbuf-spike:", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() { <-ctx.Done(); s.close() }()

	for ev := range s.events() {
		fmt.Printf("tgid=%d tid=%d fd=%d chunk %d/%d len=%d left_out=%d dev=%#x ino=%d mode=%#o %q\n",
			ev.Tgid, ev.Tid, ev.FD, ev.Seq+1, ev.NChunks, ev.Len, ev.LeftOut, ev.Dev, ev.Ino, ev.Mode, trim(ev.Data))
	}
}

func trim(b []byte) []byte {
	if len(b) > 60 {
		return b[:60]
	}
	return b
}
