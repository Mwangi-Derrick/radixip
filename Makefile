# RadixIP Makefile
.PHONY: all build test bench clean

all: build test

generate:
	@echo "Generating ipv4 and ipv6 datasets..."
	python scripts/generate_mock_data.py

clean-comments:
	@echo "Clean comments"
	cd scripts && ./clean-comments.sh


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
	@echo "  make clean  - Clean all artifacts"