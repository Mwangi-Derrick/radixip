# RadixIP XDP build helpers. Run from the ebpf/ directory.
#
# Rust:  rustup toolchain install nightly --component rust-src
#        cargo install bpf-linker
# Go:    clang, libbpf-dev, linux headers; `go get github.com/cilium/ebpf github.com/redis/go-redis/v9`

.PHONY: setup kernel loader go-gen go-test verify-unit verify all clean bench

all: kernel loader go-gen

# Reproducible L7 vs L2/L3 eBPF/XDP benchmark in isolated netns
bench:
	@chmod +x ../scripts/bench_xdp_vs_l7.sh && sudo ../scripts/bench_xdp_vs_l7.sh

# Fast checks that need no kernel object, root, or BPF capabilities.
verify-unit:
	cargo test -p xdp-loader --lib
	cargo test -p radixip-config --lib

# Full local/CI verification: build the real BPF object, then check every
# Rust consumer and run the privilege-free unit tests.
verify: setup
	$(MAKE) kernel
	cargo check -p xdp-loader --all-targets
	cargo check -p radixip-grpc-server
	$(MAKE) verify-unit

# Install the Rust toolchain components needed by the Aya builds.
setup:
	rustup toolchain install nightly --component rust-src
	cargo install bpf-linker --locked

# eBPF object. The loader embeds it with include_bytes_aligned!, so build this first.
kernel:
	rustup component add rust-src --toolchain nightly
	cd xdp-kernel && cargo +nightly build --release \
		--target bpfel-unknown-none -Z build-std=core

loader: kernel
	cd xdp-loader && cargo build --release

# Generates lib/go/xdp/xdpblocklist_bpf*.go from the C program.
go-gen:
	cd ../lib/go/xdp && go generate ./...

# BPF_PROG_TEST_RUN tests; need root (skipped otherwise).
go-test: go-gen
	cd ../lib/go/xdp && sudo -E env "PATH=$$PATH" go test -v ./...

clean:
	cd xdp-kernel && cargo clean
	cd xdp-loader && cargo clean