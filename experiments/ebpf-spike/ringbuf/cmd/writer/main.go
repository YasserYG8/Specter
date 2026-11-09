//go:build linux

// writer is the target the spike's tests watch. It does exactly what it is told on stdin, one command per line,
// and answers "done" on fd 3 (the tests pass a pipe there) when the command is finished.
//
//	write <fd> <n>      one write(2) of n bytes to fd; the bytes are a-z repeating, and the last one is a newline
//	spawn <generations> start a child writer process, which starts one more until the generations are used up;
//	                    each writes "child gen=<n> pid=<pid>" to its stdout, once
//
// The tests also give it fd 4 (a private pipe) and fd 5 (/dev/null) to write to.
package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

func pattern(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = 'a' + byte(i%26)
	}
	if n > 0 {
		b[n-1] = '\n'
	}
	return b
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "child" {
		child(os.Args[2:])
		return
	}

	reply := os.NewFile(3, "reply")
	sc := bufio.NewScanner(os.Stdin)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "write":
			fd, _ := strconv.Atoi(f[1])
			n, _ := strconv.Atoi(f[2])
			buf := pattern(n)
			if _, err := syscall.Write(fd, buf); err != nil {
				fmt.Fprintln(os.Stderr, "write:", err)
			}
		case "spawn":
			gens, _ := strconv.Atoi(f[1])
			cmd := exec.Command(os.Args[0], "child", strconv.Itoa(gens))
			cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
			if err := cmd.Run(); err != nil {
				fmt.Fprintln(os.Stderr, "spawn:", err)
			}
		}
		fmt.Fprintln(reply, "done")
	}
}

// child writes one line to its stdout and, if generations remain, starts the next one.
func child(args []string) {
	gens, _ := strconv.Atoi(args[0])
	line := fmt.Sprintf("child gen=%d pid=%d\n", gens, os.Getpid())
	_, _ = syscall.Write(1, []byte(line))
	if gens > 1 {
		cmd := exec.Command(os.Args[0], "child", strconv.Itoa(gens-1))
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		_ = cmd.Run()
	}
}
