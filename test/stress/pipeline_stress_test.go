package stress

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/specter-cli/specter/internal/pipeline"
	"github.com/specter-cli/specter/internal/source"
)

func TestStress_PipelineBurst10k(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const totalLines = 10000

	chunks := make(chan source.Chunk, 1024)

	// Pipeline stages (use 5s idle timeout to tolerate race detector instrumentation overhead on 10k lines)
	assembler := pipeline.NewLineAssembler(5 * time.Second)
	lines := assembler.Assemble(ctx, chunks)

	grouper := pipeline.NewStackTraceGrouper(5 * time.Second)
	entries := grouper.Group(ctx, lines)

	filter := pipeline.NewFilter(pipeline.FilterOptions{
		MinLevel: pipeline.SeverityDebug,
	})

	renderer := pipeline.NewRenderer(pipeline.RendererOptions{
		NoColor: true,
	})

	// Consumer goroutine
	renderedCount := 0
	receivedSeqs := make([]bool, totalLines)
	var consumerWG sync.WaitGroup
	consumerWG.Add(1)

	go func() {
		defer consumerWG.Done()
		for entry := range entries {
			if !filter.Matches(&entry) {
				continue
			}
			rendered := renderer.Render(&entry)
			if strings.Contains(rendered, "burst_seq=") {
				var seq int
				idx := strings.Index(rendered, "burst_seq=")
				if idx != -1 {
					sub := rendered[idx+len("burst_seq="):]
					endIdx := strings.IndexAny(sub, " \t\r\n")
					if endIdx == -1 {
						endIdx = len(sub)
					}
					if _, err := fmt.Sscanf(sub[:endIdx], "%d", &seq); err == nil && seq >= 0 && seq < totalLines {
						receivedSeqs[seq] = true
						renderedCount++
					}
				}
			}
		}
	}()

	// Producer: emit 10,000 lines with deliberate chunk fragmentation
	startTime := time.Now()

	for i := 0; i < totalLines; i++ {
		lineText := fmt.Sprintf(`{"level":"info","msg":"high throughput stress line %d","burst_seq":%d}`+"\n", i, i)
		bytes := []byte(lineText)

		if i%3 == 0 && len(bytes) > 20 {
			// Split across 2 chunks to stress fragmentation handling
			mid := len(bytes) / 2
			chunks <- source.Chunk{Bytes: bytes[:mid], FD: 1, PID: 100, TS: time.Now()}
			chunks <- source.Chunk{Bytes: bytes[mid:], FD: 1, PID: 100, TS: time.Now()}
		} else {
			chunks <- source.Chunk{Bytes: bytes, FD: 1, PID: 100, TS: time.Now()}
		}
	}
	close(chunks)

	consumerWG.Wait()
	duration := time.Since(startTime)

	t.Logf("Processed %d / %d lines in %v (throughput: %.0f lines/sec)",
		renderedCount, totalLines, duration, float64(renderedCount)/duration.Seconds())

	if renderedCount != totalLines {
		t.Errorf("expected %d rendered lines, but received %d", totalLines, renderedCount)
	}

	missing := 0
	var missingList []int
	for i := 0; i < totalLines; i++ {
		if !receivedSeqs[i] {
			missing++
			if len(missingList) < 20 {
				missingList = append(missingList, i)
			}
		}
	}
	if missing > 0 {
		t.Errorf("missing %d sequence numbers out of %d (first 20: %v)", missing, totalLines, missingList)
	}
}

func TestStress_ConcurrentInterleaved(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	const (
		numWorkers     = 8
		linesPerWorker = 1000
	)

	chunks := make(chan source.Chunk, 2048)

	assembler := pipeline.NewLineAssembler(50 * time.Millisecond)
	lines := assembler.Assemble(ctx, chunks)

	grouper := pipeline.NewStackTraceGrouper(50 * time.Millisecond)
	entries := grouper.Group(ctx, lines)

	filter := pipeline.NewFilter(pipeline.FilterOptions{})
	renderer := pipeline.NewRenderer(pipeline.RendererOptions{NoColor: true})

	var consumerWG sync.WaitGroup
	consumerWG.Add(1)

	var (
		mu                 sync.Mutex
		receivedNormal     int
		receivedStacktrace int
	)

	go func() {
		defer consumerWG.Done()
		for entry := range entries {
			if !filter.Matches(&entry) {
				continue
			}
			_ = renderer.Render(&entry)
			mu.Lock()
			if entry.IsTrace {
				receivedStacktrace++
			} else {
				receivedNormal++
			}
			mu.Unlock()
		}
	}()

	var producerWG sync.WaitGroup
	now := time.Now()

	for w := 0; w < numWorkers; w++ {
		producerWG.Add(1)
		workerID := w
		pid := 1000 + workerID

		go func() {
			defer producerWG.Done()
			for i := 0; i < linesPerWorker; i++ {
				if workerID == 0 && i%50 == 0 {
					// Worker 0 periodically emits multi-line stack traces on stderr
					chunks <- source.Chunk{
						Bytes: []byte("Traceback (most recent call last):\n"),
						FD:    2,
						PID:   pid,
						TS:    now,
					}
					chunks <- source.Chunk{
						Bytes: []byte("  File \"worker.py\", line 42, in process\n    raise RuntimeError(\"stress test\")\n"),
						FD:    2,
						PID:   pid,
						TS:    now,
					}
					chunks <- source.Chunk{
						Bytes: []byte("RuntimeError: stress test\n"),
						FD:    2,
						PID:   pid,
						TS:    now,
					}
				} else {
					// Normal stdout log
					chunks <- source.Chunk{
						Bytes: []byte(fmt.Sprintf("worker=%d seq=%d status=200 OK\n", workerID, i)),
						FD:    1,
						PID:   pid,
						TS:    now,
					}
				}
			}
		}()
	}

	producerWG.Wait()
	close(chunks)
	consumerWG.Wait()

	mu.Lock()
	normal := receivedNormal
	traces := receivedStacktrace
	mu.Unlock()

	t.Logf("Concurrent test complete: %d normal lines, %d stack traces", normal, traces)
	if normal == 0 {
		t.Errorf("expected normal lines to be processed, got 0")
	}
	if traces == 0 {
		t.Errorf("expected stack traces to be grouped, got 0")
	}
}

func TestStress_ResourceTeardown(t *testing.T) {
	initialGoroutines := runtime.NumGoroutine()

	ctx, cancel := context.WithCancel(context.Background())
	chunks := make(chan source.Chunk, 100)

	assembler := pipeline.NewLineAssembler(20 * time.Millisecond)
	lines := assembler.Assemble(ctx, chunks)

	grouper := pipeline.NewStackTraceGrouper(20 * time.Millisecond)
	entries := grouper.Group(ctx, lines)

	// Feed 1000 items
	go func() {
		for i := 0; i < 1000; i++ {
			chunks <- source.Chunk{
				Bytes: []byte(fmt.Sprintf("log %d\n", i)),
				FD:    1,
				PID:   50,
				TS:    time.Now(),
			}
		}
		close(chunks)
	}()

	// Drain entries
	for range entries {
	}

	cancel()

	// Allow goroutines to exit
	time.Sleep(100 * time.Millisecond)

	finalGoroutines := runtime.NumGoroutine()
	// Allow small delta for runtime background workers
	if finalGoroutines > initialGoroutines+3 {
		t.Errorf("potential goroutine leak: initial=%d, final=%d", initialGoroutines, finalGoroutines)
	}
}
