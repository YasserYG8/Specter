package main

import (
	"bytes"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"
)

func main() {
	portFlag := flag.Int("port", 0, "Port to listen on (0 for dynamic ephemeral port)")
	roleFlag := flag.String("role", "parent", "Role: parent or child")
	spawnChildFlag := flag.Bool("spawn-child", false, "Spawn child worker at startup")
	workerIDFlag := flag.Int("id", 0, "Worker id for the numbered-writer role")
	workerLinesFlag := flag.Int("lines", 0, "Lines to write for the numbered-writer role")
	workersFlag := flag.Int("workers", 0, "Start this many numbered-writer workers before the ready line")
	waitFileFlag := flag.String("wait-file", "", "Numbered-writer workers wait for this file to exist before writing")
	flag.Parse()

	if *roleFlag == "child" || *roleFlag == "child-worker" {
		runChildWorker()
		return
	}
	if *roleFlag == "numbered-writer" {
		runNumberedWriter(*workerIDFlag, *workerLinesFlag, *waitFileFlag)
		return
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *portFlag))
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: failed to listen on port %d: %v\n", *portFlag, err)
		os.Exit(1)
	}
	defer listener.Close()

	actualPort := listener.Addr().(*net.TCPAddr).Port

	if *spawnChildFlag {
		childCmd := exec.Command(os.Args[0], "-role=child-worker")
		childCmd.Stdout = os.Stdout
		childCmd.Stderr = os.Stderr
		if err := childCmd.Start(); err == nil {
			go func() {
				_ = childCmd.Wait()
			}()
		}
	}

	// Workers started here exist before the harness attaches Specter, so Specter has to find them by
	// scanning for child processes, which strace -f cannot do for it.
	for i := 0; i < *workersFlag; i++ {
		cmd := exec.Command(os.Args[0], "-role=numbered-writer", fmt.Sprintf("-id=%d", i),
			fmt.Sprintf("-lines=%d", *workerLinesFlag), "-wait-file="+*waitFileFlag)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			fmt.Fprintf(os.Stderr, "FATAL: failed to start worker %d: %v\n", i, err)
			os.Exit(1)
		}
		go func() { _ = cmd.Wait() }()
	}

	// Signal ready to stdout for test harness synchronization
	fmt.Printf("SPECTER_TEST_SERVER_READY port=%d pid=%d\n", actualPort, os.Getpid())
	os.Stdout.Sync()

	mux := http.NewServeMux()

	// 1. Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "OK")
	})

	// 2. Structured JSON log emission
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		level := r.URL.Query().Get("level")
		if level == "" {
			level = "info"
		}
		msg := r.URL.Query().Get("msg")
		if msg == "" {
			msg = "request processed"
		}
		jsonLog := fmt.Sprintf(`{"level":%q,"msg":%q,"path":%q,"status":200,"ts":%d}`,
			level, msg, r.URL.Path, time.Now().UnixNano())
		fmt.Fprintln(os.Stdout, jsonLog)
		os.Stdout.Sync()

		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "JSON log emitted")
	})

	// 3. Multi-line error trace emission on stderr (fd 2)
	mux.HandleFunc("/error", func(w http.ResponseWriter, r *http.Request) {
		trace := "Traceback (most recent call last):\n" +
			"  File \"/app/server.py\", line 42, in handle_request\n" +
			"    process_order(order_id)\n" +
			"  File \"/app/orders.py\", line 18, in process_order\n" +
			"    raise ValueError(\"payment authorization failed\")\n" +
			"ValueError: payment authorization failed\n"
		fmt.Fprint(os.Stderr, trace)
		os.Stderr.Sync()

		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintln(w, "Error trace emitted")
	})

	// 4. Burst log emission
	mux.HandleFunc("/burst", func(w http.ResponseWriter, r *http.Request) {
		countStr := r.URL.Query().Get("count")
		count, _ := strconv.Atoi(countStr)
		if count <= 0 {
			count = 100
		}
		for i := 0; i < count; i++ {
			fmt.Fprintf(os.Stdout, `{"level":"info","msg":"burst message %d","seq":%d}`+"\n", i, i)
		}
		os.Stdout.Sync()

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Emitted %d burst logs\n", count)
	})

	// 5. Fork child worker process for multi-process tree testing
	mux.HandleFunc("/fork", func(w http.ResponseWriter, r *http.Request) {
		cmd := exec.Command(os.Args[0], "-role=child")
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		// Let child run asynchronously
		go func() {
			_ = cmd.Wait()
		}()

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Child worker forked with PID %d\n", cmd.Process.Pid)
	})

	// 5b. Several worker processes that each write numbered lines (w<id>-<seq>) to stdout, one write per line
	mux.HandleFunc("/workers", func(w http.ResponseWriter, r *http.Request) {
		count, _ := strconv.Atoi(r.URL.Query().Get("count"))
		lines, _ := strconv.Atoi(r.URL.Query().Get("lines"))
		if count <= 0 {
			count = 4
		}
		if lines <= 0 {
			lines = 3000
		}
		var cmds []*exec.Cmd
		for i := 0; i < count; i++ {
			cmd := exec.Command(os.Args[0], "-role=numbered-writer", fmt.Sprintf("-id=%d", i), fmt.Sprintf("-lines=%d", lines))
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Start(); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			cmds = append(cmds, cmd)
		}
		for _, cmd := range cmds {
			_ = cmd.Wait()
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "%d workers wrote %d lines each\n", count, lines)
	})

	// 5c. One line of N bytes, written with a single write, followed by a short line
	mux.HandleFunc("/longline", func(w http.ResponseWriter, r *http.Request) {
		size, _ := strconv.Atoi(r.URL.Query().Get("bytes"))
		if size <= 0 {
			size = 20000
		}
		line := bytes.Repeat([]byte("A"), size)
		line = append(line, '\n')
		_, _ = os.Stdout.Write(line)
		_, _ = os.Stdout.WriteString("after-long-line\n")

		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Wrote a line of %d bytes\n", size)
	})

	// 5d. A log line written with writev(2) instead of write(2)
	mux.HandleFunc("/writev", func(w http.ResponseWriter, r *http.Request) {
		msg := r.URL.Query().Get("msg")
		if msg == "" {
			msg = "writev-default"
		}
		_ = writevStdout([]byte("writev-line: "), []byte(msg), []byte("\n"))

		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "writev emitted")
	})

	// 6. Graceful target termination endpoint
	mux.HandleFunc("/exit", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "Exiting server")
		go func() {
			time.Sleep(50 * time.Millisecond)
			os.Exit(0)
		}()
	})

	server := &http.Server{Handler: mux}
	_ = server.Serve(listener)
}

func runChildWorker() {
	// Worker child process emits identifiable logs periodically
	for i := 0; i < 50; i++ {
		fmt.Fprintf(os.Stdout, `{"level":"info","msg":"child worker active","worker_pid":%d,"step":%d}`+"\n", os.Getpid(), i)
		os.Stdout.Sync()
		time.Sleep(150 * time.Millisecond)
	}
}

// runNumberedWriter writes lines "w<id>-<seq>" to stdout, one write(2) per line, so a test can compare the
// lines written with the lines captured. When waitFile is set, it first waits for that file to exist, so a test
// can attach Specter while the worker is already running and then release it.
func runNumberedWriter(id, lines int, waitFile string) {
	if waitFile != "" {
		deadline := time.Now().Add(2 * time.Minute)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(waitFile); err == nil {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
	}
	for i := 0; i < lines; i++ {
		_, _ = os.Stdout.WriteString(fmt.Sprintf("w%d-%d\n", id, i))
	}
}
