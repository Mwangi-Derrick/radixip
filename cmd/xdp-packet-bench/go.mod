module xdp-packet-bench

go 1.27.2

require github.com/Mwangi-Derrick/radixip/lib/go/xdp v0.0.0-00010101000000-000000000000

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/cilium/ebpf v0.22.0 // indirect
	github.com/redis/go-redis/v9 v9.23.0 // indirect
	go.uber.org/atomic v1.12.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
)

replace github.com/Mwangi-Derrick/radixip/lib/go/xdp => ../../lib/go/xdp
