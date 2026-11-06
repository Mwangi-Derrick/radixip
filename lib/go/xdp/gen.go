package xdp

// Generates xdpblocklist_bpfel.go / _bpfeb.go (+ .o) from the C program.
// Needs: clang, libbpf-dev (headers), kernel UAPI headers.
// Re-run `go generate ./...` after any change to xdp_blocklist.c.
//
//go:generate go run github.com/cilium/ebpf/cmd/bpf2go -cc clang -cflags "-O2 -g -Wall -Werror" xdpBlocklist ../../../ebpf/xdp-c/xdp_blocklist.c -- -I/usr/include/bpf