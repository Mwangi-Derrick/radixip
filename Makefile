# RadixIP Makefile
.PHONY: all build test bench clean

all: build test

generate:
	@echo "Generating ipv4 and ipv6 datasets..."
	python scripts/generate_mock_data.py

generate_protobuf_go:
	@echo "Generating protobuf files..."
	protoc --go_out=proto/radixip/v1 --go_opt=paths=source_relative \
	    --go-grpc_out=proto/radixip/v1 --go-grpc_opt=paths=source_relative \
	    -I=proto/radixip/v1 proto/radixip/v1/radixip.proto



build:
	@echo "Building Rust core..."
	cd lib/rust && cargo build --release
	@echo "Building Python bindings..."
	cd lib/python && maturin build
	@echo "Building Node.js bindings..."
	cd lib/node && npm run build
	@echo "Building CLI..."
	cd cmd/radixip-cli && cargo build --release

test:
	@echo "Testing Rust..."
	cd lib/rust && cargo test
	@echo "Testing Python..."
	cd lib/python && pytest
	@echo "Testing Node..."
	cd lib/node && npm test

bench:
	@echo "Benchmarking Rust..."
	cd lib/rust && cargo bench
	@echo "Benchmarking Go..."
	cd lib/go && go test -bench=. -benchmem ./...

clean:
	@echo "Cleaning..."
	cd lib/rust && cargo clean
	cd lib/python && maturin clean
	cd lib/node && rm -rf node_modules
	cd cmd/radixip-cli && cargo clean

help:
	@echo "Commands:"	
	@echo "  make all    - Build and test everything"
	@echo "  make generate - Generate mock data"
	@echo "  make build  - Build all implementations"
	@echo "  make test   - Test all implementations"
	@echo "  make bench  - Run all benchmarks"
	@echo "  make generate_protobuf_go - Generate protobuf files for go"
	@echo "  make clean  - Clean all artifacts"