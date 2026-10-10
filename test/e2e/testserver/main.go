package main

import (
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
	flag.Parse()

	if *roleFlag == "child" {
		runChildWorker()
		return
	}

	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *portFlag))
	if err != nil {
		fmt.Fprintf(os.Stderr, "FATAL: failed to listen on port %d: %v\n", *portFlag, err)
		os.Exit(1)
	}
	defer listener.Close()

	actualPort := listener.Addr().(*net.TCPAddr).Port
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
	// Worker child process emits identifiable logs and exits
	for i := 0; i < 15; i++ {
		fmt.Fprintf(os.Stdout, `{"level":"info","msg":"child worker active","worker_pid":%d,"step":%d}`+"\n", os.Getpid(), i)
		os.Stdout.Sync()
		time.Sleep(100 * time.Millisecond)
	}
}
