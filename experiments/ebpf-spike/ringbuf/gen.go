//go:build linux

package main

// The BPF program is compiled once and the result is committed (spike_bpfel.o and spike_bpfel.go), so building
// needs no clang. To regenerate after changing bpf/spike.c, install clang and libbpf-dev and run: go generate
//
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -I/usr/include/x86_64-linux-gnu" -target bpfel spike bpf/spike.c
