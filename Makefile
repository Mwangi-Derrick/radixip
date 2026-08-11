# RadixIP Makefile
.PHONY: all build test bench clean \
        build-simd-ffi build-go-simd test-go-simd \
        install-cbindgen check-cbindgen

# ────────────────────────────────────────────────────────────────────────────
# Helpers: detect OS for shared-object extension and copy target
# ────────────────────────────────────────────────────────────────────────────
UNAME_S := $(shell uname -s 2>/dev/null || echo Windows)
ifeq ($(UNAME_S),Darwin)
  SOEXT     := dylib
  SOPREFIX  := lib
else ifeq ($(UNAME_S),Windows)
  SOEXT     := dll
  SOPREFIX  :=
else
  SOEXT     := so
  SOPREFIX  := lib
endif

SIMD_FFI_CRATE := lib/rust/node16_simd_ffi
SIMD_FFI_SO    := $(SIMD_FFI_CRATE)/target/release/$(SOPREFIX)node16_simd_ffi.$(SOEXT)
VENDOR_LIB     := lib/vendor/lib
VENDOR_INC     := lib/vendor/include

all: build test

generate:
	@echo "Generating ipv4 and ipv6 datasets..."
	python scripts/generate_mock_data.py

generate_protobuf_go:
	@echo "Generating protobuf files..."
	protoc --go_out=proto/radixip/v1 --go_opt=paths=source_relative \
	    --go-grpc_out=proto/radixip/v1 --go-grpc_opt=paths=source_relative \
	    -I=proto/radixip/v1 proto/radixip/v1/radixip.proto

# ────────────────────────────────────────────────────────────────────────────
# cbindgen: check presence, offer to install if missing
# ────────────────────────────────────────────────────────────────────────────
check-cbindgen:
	@if ! command -v cbindgen > /dev/null 2>&1; then \
	    if ! cargo install --list 2>/dev/null | grep -q "^cbindgen "; then \
	        echo "cbindgen not found on PATH and not installed via cargo."; \
	        echo "Installing cbindgen (this may take a minute)..."; \
	        cargo install cbindgen; \
	    else \
	        echo "cbindgen is installed via cargo but not on PATH."; \
	        echo "Add \$$(cargo root)/bin to your PATH, e.g.:"; \
	        echo "  export PATH=\"\$$(cargo root)/bin:\$$PATH\""; \
	        exit 1; \
	    fi; \
	fi

# ────────────────────────────────────────────────────────────────────────────
# build-simd-ffi: compile the Rust cdylib and copy artifacts to vendor/
# ────────────────────────────────────────────────────────────────────────────
build-simd-ffi: check-cbindgen
	@echo "Building SIMD FFI shared library (Rust)..."
	cd $(SIMD_FFI_CRATE) && cargo build --release
	@echo "Copying shared object to $(VENDOR_LIB)/..."
	mkdir -p $(VENDOR_LIB)
	cp -f $(SIMD_FFI_SO) $(VENDOR_LIB)/
	@echo "Shared library ready: $(VENDOR_LIB)/$(SOPREFIX)node16_simd_ffi.$(SOEXT)"
	@echo "Header written to   : $(VENDOR_INC)/node16_simd.h"

# ────────────────────────────────────────────────────────────────────────────
# build-go-simd: build the Go ART package with the CGo SIMD bridge active
# ────────────────────────────────────────────────────────────────────────────
build-go-simd: build-simd-ffi
	@echo "Building Go ART with CGo SIMD bridge (-tags simd_cgo)..."
	cd lib/go && CGO_ENABLED=1 go build -tags simd_cgo ./...

# ────────────────────────────────────────────────────────────────────────────
# test-go-simd: run Go ART tests with the CGo SIMD bridge
# ────────────────────────────────────────────────────────────────────────────
test-go-simd: build-simd-ffi
	@echo "Testing Go ART with CGo SIMD bridge (-tags simd_cgo)..."
	cd lib/go && CGO_ENABLED=1 go test -tags simd_cgo -v ./art/...

# ────────────────────────────────────────────────────────────────────────────
# Standard targets (unchanged behaviour, extended with SIMD step)
# ────────────────────────────────────────────────────────────────────────────
build: build-simd-ffi
	@echo "Building Rust core..."
	cd lib/rust && cargo build --release
	@echo "Building Python bindings..."
	cd lib/python && maturin build
	@echo "Building Node.js bindings..."
	cd lib/node && npm run build
	@echo "Building CLI..."
	cd cmd/radixip-cli && cargo build --release

build-grpc:
	@echo "Building Go grpc server..."
	cd cmd/grpc-server-go && go build -o radixip-grpc-go

build-grpc-release:
	@echo "Building Go grpc server..."
	cd cmd/grpc-server-go && go build -o radixip-grpc-go -race
	@echo "Building rust grpc server..."
	cd cmd/grpc-server-rust && cargo build --release --bin grpc-server-rust

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
	cd $(SIMD_FFI_CRATE) && cargo clean
	cd lib/python && maturin clean
	cd lib/node && rm -rf node_modules
	cd cmd/radixip-cli && cargo clean
	rm -f $(VENDOR_LIB)/$(SOPREFIX)node16_simd_ffi.$(SOEXT)

help:
	@echo "Commands:"
	@echo "  make all              - Build and test everything"
	@echo "  make generate         - Generate mock data"
	@echo "  make build            - Build all implementations (includes SIMD FFI)"
	@echo "  make test             - Test all implementations"
	@echo "  make bench            - Run all benchmarks"
	@echo "  ─────────────── SIMD ────────────────────────────────────────────"
	@echo "  make build-simd-ffi   - Compile Rust SIMD shared library + copy to vendor/"
	@echo "  make build-go-simd    - Build Go ART with CGo SIMD bridge"
	@echo "  make test-go-simd     - Run Go ART tests with CGo SIMD bridge"
	@echo "  make check-cbindgen   - Verify cbindgen is installed (install if not)"
	@echo "  ─────────────────────────────────────────────────────────────────"
	@echo "  make generate_protobuf_go - Generate protobuf files for go"
	@echo "  make clean            - Clean all artifacts"
	@echo "  make build-grpc       - Build Go grpc server"
	@echo "  make build-grpc-release - Build Go grpc and Rust server release"