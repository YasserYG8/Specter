//go:build linux

package ebpf

// The BPF program in bpf/specter.c is compiled ahead of time, and the result (specter_bpfel.o and
// specter_bpfel.go) is committed, so `go build` and the releases need no C compiler and stay static with
// CGO_ENABLED=0. Only a developer who changes bpf/specter.c needs clang, llvm and libbpf-dev:
//
//	sudo apt install clang llvm libbpf-dev
//	go generate ./internal/source/ebpf
//
// One little-endian object serves amd64 and arm64: the program uses only CO-RE field reads and syscall tracepoint
// layouts, which do not depend on the architecture. CI regenerates the files with a pinned clang and fails if they
// differ from the committed ones.
//
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror -I/usr/include/x86_64-linux-gnu" -tags linux -type config -type event -target bpfel specter bpf/specter.c
