#!/usr/bin/env bash
# scripts/bench_xdp_vs_l7.sh
#
# Measures gRPC RPC-call throughput and XDP packet drops for BOTH Go and Rust.
# - Go XDP stack: Cilium eBPF + C BPF program (xdp-c)
# - Rust XDP stack: Aya eBPF + Rust BPF program (xdp-kernel)
# XDP PPS is calculated from each program's per-CPU drop-counter delta and
# wall-clock packet-generator duration across isolated network namespaces.
#
# Runs on any Linux OS (Fedora Workstation, Ubuntu, Arch, Debian) using isolated
# network namespaces and veth pairs. Safe to run without dropping host networking.
#
# Usage:
#   sudo ./scripts/bench_xdp_vs_l7.sh [all|go|rust]
#
# Requires: sudo / root privileges, Go, Cargo, iproute2, and iputils-ping.

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

TARGET="${1:-all}" # "all", "go", or "rust"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

if [[ $EUID -ne 0 ]]; then
    echo -e "${RED}❌ This script must be run as root (or via sudo) to configure network namespaces and attach XDP.${NC}"
    echo "Usage: sudo ./scripts/bench_xdp_vs_l7.sh [all|go|rust]"
    exit 1
fi

if ! command -v go &>/dev/null && [[ -x /usr/local/go/bin/go ]]; then
    PATH="/usr/local/go/bin:${PATH}"
    export PATH
fi

if [[ -n "${SUDO_USER:-}" ]]; then
    SUDO_USER_HOME="$(getent passwd "${SUDO_USER}" | cut -d: -f6)"
    if [[ -x "${SUDO_USER_HOME}/.cargo/bin/cargo" ]]; then
        PATH="${SUDO_USER_HOME}/.cargo/bin:${PATH}"
        export PATH
    fi
    if [[ -d "${SUDO_USER_HOME}/.cargo" ]]; then
        CARGO_HOME="${SUDO_USER_HOME}/.cargo"
        export CARGO_HOME
    fi
    if [[ -d "${SUDO_USER_HOME}/.rustup" ]]; then
        RUSTUP_HOME="${SUDO_USER_HOME}/.rustup"
        export RUSTUP_HOME
    fi
fi

NS_CLIENT="radix-bench-cli"
NS_SERVER="radix-bench-srv"
VETH_CLI="veth-c"
VETH_SRV="veth-s"
IP_CLI="10.200.1.2"
IP_SRV="10.200.1.1"
PORT=50051
CONCURRENCY=32
RESULTS_DIR="${ROOT_DIR}/test-results/bench_l7_vs_xdp"
XDP_DURATION="${XDP_DURATION:-5}"
mkdir -p "${RESULTS_DIR}"

cleanup() {
    echo -e "\n${YELLOW}🧹 Cleaning up namespaces and background processes...${NC}"
    pkill -f "radixip-grpc-go|radixip-grpc-server|grpc-server-rust" 2>/dev/null || true
    ip netns del "${NS_CLIENT}" 2>/dev/null || true
    ip netns del "${NS_SERVER}" 2>/dev/null || true
    rm -rf /sys/fs/bpf/radixip_bench 2>/dev/null || true
}
trap cleanup EXIT

echo -e "${BOLD}${BLUE}========================================================================${NC}"
echo -e "${BOLD}${BLUE}  RadixIP Performance Benchmark: L7 vs L2/L3 eBPF/XDP (Go & Rust)      ${NC}"
echo -e "${BOLD}${BLUE}  Target Runtimes: ${BOLD}${TARGET^^}${NC}"
echo -e "${BOLD}${BLUE}========================================================================${NC}"

# Check dependencies
for tool in ip go ping; do
    if ! command -v $tool &>/dev/null; then
        echo -e "${RED}❌ Missing required tool: $tool${NC}"
        exit 1
    fi
done

# Build test probes and servers
echo -e "${BLUE}🔨 Building test probes and gRPC servers...${NC}"
mkdir -p "${ROOT_DIR}/bin"
(cd cmd/grpc-probe-go && go build -o "${ROOT_DIR}/bin/grpc-probe-go" .)

if [[ "${TARGET}" == "all" || "${TARGET}" == "go" ]]; then
    echo -e "${BLUE}🔨 Compiling Go gRPC server & Go XDP packet-bench...${NC}"
    (cd cmd/grpc-server-go && go build -o "${ROOT_DIR}/bin/radixip-grpc-go" .)
    (cd cmd/xdp-packet-bench && go build -o "${ROOT_DIR}/bin/go-xdp-packet-bench" .)
fi

if [[ "${TARGET}" == "all" || "${TARGET}" == "rust" ]]; then
    if command -v cargo &>/dev/null; then
        echo -e "${BLUE}🔨 Compiling Rust gRPC server & Rust XDP packet-bench (--release)...${NC}"
        cargo build --release -p radixip-grpc-server -p xdp-loader --bin packet-bench
        cp -f target/release/radixip-grpc-server "${ROOT_DIR}/bin/radixip-grpc-server" 2>/dev/null || \
        cp -f target/release/grpc-server-rust "${ROOT_DIR}/bin/radixip-grpc-server" 2>/dev/null || true
        cp -f target/release/packet-bench "${ROOT_DIR}/bin/rust-xdp-packet-bench"
    else
        echo -e "${YELLOW}⚠️  Cargo not found; skipping Rust server build.${NC}"
        if [[ "${TARGET}" == "rust" ]]; then
            exit 1
        fi
    fi
fi

setup_namespaces() {
    ip netns del "${NS_CLIENT}" 2>/dev/null || true
    ip netns del "${NS_SERVER}" 2>/dev/null || true

    ip netns add "${NS_CLIENT}"
    ip netns add "${NS_SERVER}"

    ip link add "${VETH_CLI}" type veth peer name "${VETH_SRV}"
    ip link set "${VETH_CLI}" netns "${NS_CLIENT}"
    ip link set "${VETH_SRV}" netns "${NS_SERVER}"

    ip netns exec "${NS_CLIENT}" ip addr add "${IP_CLI}/24" dev "${VETH_CLI}"
    ip netns exec "${NS_CLIENT}" ip link set "${VETH_CLI}" up
    ip netns exec "${NS_CLIENT}" ip link set lo up

    ip netns exec "${NS_SERVER}" ip addr add "${IP_SRV}/24" dev "${VETH_SRV}"
    ip netns exec "${NS_SERVER}" ip link set "${VETH_SRV}" up
    ip netns exec "${NS_SERVER}" ip link set lo up

    ip netns exec "${NS_CLIENT}" ping -c 1 -W 1 "${IP_SRV}" >/dev/null
}

run_probe() {
    local label=$1
    local blocked_ip=$2
    local total_reqs=${3:-15000}

    START_TIME=$(date +%s%N)
    
    OUT=$(ip netns exec "${NS_CLIENT}" "${ROOT_DIR}/bin/grpc-probe-go" \
        -target "${IP_SRV}:${PORT}" \
        -requests "${total_reqs}" \
        -concurrency "${CONCURRENCY}" \
        -ip "${blocked_ip}" 2>&1)
    
    END_TIME=$(date +%s%N)
    ELAPSED_MS=$(( (END_TIME - START_TIME) / 1000000 ))
    
    RPS=0
    if [[ $ELAPSED_MS -gt 0 ]]; then
        RPS=$(( (total_reqs * 1000) / ELAPSED_MS ))
    fi

    echo "${OUT}" > "${RESULTS_DIR}/${label}.txt"
    echo "${RPS}"
}

run_xdp_packet_benchmark() {
    local runtime=$1 # "go" or "rust"
    local mode=$2    # "skb" or "drv"
    local bench_bin="${ROOT_DIR}/bin/${runtime}-xdp-packet-bench"
    local label="xdp_${runtime}_${mode}"
    local result_file="${RESULTS_DIR}/${label}.txt"
    local output
    local status=0

    if [[ ! -f "${bench_bin}" ]]; then
        echo -e "${YELLOW}⚠️  Binary ${bench_bin} not found; skipping ${runtime} XDP ${mode}.${NC}"
        return
    fi

    setup_namespaces
    echo -e "${BLUE}▶ Measuring [${runtime^^}] XDP ${mode} packet drops on server veth...${NC}"
    local sender_bin="${ROOT_DIR}/bin/go-xdp-packet-bench"
    output=$(ip netns exec "${NS_SERVER}" "${bench_bin}" \
        -iface "${VETH_SRV}" \
        -source "${IP_CLI}" \
        -destination "${IP_SRV}" \
        -client-netns "${NS_CLIENT}" \
        -mode "${mode}" \
        -duration "${XDP_DURATION}" \
        -sender-bin "${sender_bin}" 2>&1) || status=$?
    printf '%s\n' "${output}" | tee "${result_file}"

    if [[ ${status} -eq 0 ]]; then
        local pps
        pps=$(sed -n 's/.*xdp_drop_pps=\([0-9][0-9]*\).*/\1/p' <<<"${output}" | tail -n 1)
        if [[ -z "${pps}" ]]; then
            echo -e "${YELLOW}XDP benchmark returned no measured PPS value.${NC}"
            if [[ "${mode}" == "skb" ]]; then
                RESULTS_XDP_SKB["${runtime^^}"]="INVALID RESULT"
            else
                RESULTS_XDP_DRV["${runtime^^}"]="INVALID RESULT"
            fi
            return
        fi
        if [[ "${mode}" == "skb" ]]; then
            RESULTS_XDP_SKB["${runtime^^}"]="${pps} pps"
        else
            RESULTS_XDP_DRV["${runtime^^}"]="${pps} pps"
        fi
        echo -e "   Measured XDP drops: ${BOLD}${GREEN}${pps} packets/s${NC} (per-CPU counter delta / ${XDP_DURATION}s flood)"
    else
        echo -e "${YELLOW}XDP ${runtime} ${mode} measurement failed (exit ${status}); see ${result_file}.${NC}"
        if [[ "${mode}" == "skb" ]]; then
            RESULTS_XDP_SKB["${runtime^^}"]="UNAVAILABLE"
        else
            RESULTS_XDP_DRV["${runtime^^}"]="UNAVAILABLE"
        fi
    fi
}

declare -A RESULTS_BASELINE
declare -A RESULTS_L7
declare -A RESULTS_XDP_SKB
declare -A RESULTS_XDP_DRV

run_suite() {
    local RUNTIME=$1
    local BINARY=$2

    echo -e "\n${BOLD}${BLUE}========================================================================${NC}"
    echo -e "${BOLD}${BLUE}  Starting gRPC Benchmark Suite for: ${BOLD}${RUNTIME}${NC}"
    echo -e "${BOLD}${BLUE}========================================================================${NC}"

    if [[ ! -f "${BINARY}" ]]; then
        echo -e "${YELLOW}⚠️  Binary ${BINARY} not found; skipping ${RUNTIME}.${NC}"
        return
    fi

    # 1. Baseline (Unblocked)
    setup_namespaces
    echo -e "${BLUE}▶ [${RUNTIME}] 1. Baseline: Allowed gRPC Traffic...${NC}"
    pkill -f "$(basename "${BINARY}")" 2>/dev/null || true
    sleep 1

    ip netns exec "${NS_SERVER}" env \
        GRPC_PORT="${PORT}" \
        METRICS_PORT="9090" \
        RADIXIP_L7_BLOCK_CLIENT="false" \
        "${BINARY}" > "${RESULTS_DIR}/${RUNTIME}_server.log" 2>&1 &
    PID=$!
    sleep 2

    RPS=$(run_probe "${RUNTIME}_01_baseline" "192.0.2.10" 15000)
    RESULTS_BASELINE["${RUNTIME}"]="${RPS}"
    echo -e "   RPC calls: ${BOLD}${GREEN}${RPS} req/s${NC}"
    kill $PID 2>/dev/null || true

    # 2. L7 Interceptor Block
    setup_namespaces
    echo -e "${BLUE}▶ [${RUNTIME}] 2. L7 Blocklist: Application Interceptor Drop (403)...${NC}"
    ip netns exec "${NS_SERVER}" env \
        GRPC_PORT="${PORT}" \
        METRICS_PORT="9090" \
        RADIXIP_L7_BLOCK_CLIENT="true" \
        "${BINARY}" >> "${RESULTS_DIR}/${RUNTIME}_server.log" 2>&1 &
    PID=$!
    sleep 2

    RPS=$(run_probe "${RUNTIME}_02_l7_blocked" "203.0.113.250" 15000)
    RESULTS_L7["${RUNTIME}"]="${RPS}"
    echo -e "   RPC calls: ${BOLD}${YELLOW}${RPS} req/s${NC} (TCP handshake & HTTP/2 parsed)"
    kill $PID 2>/dev/null || true
}

# Run gRPC Benchmarks
if [[ "${TARGET}" == "all" || "${TARGET}" == "go" ]]; then
    run_suite "Go" "${ROOT_DIR}/bin/radixip-grpc-go"
fi

if [[ "${TARGET}" == "all" || "${TARGET}" == "rust" ]]; then
    if [[ -f "${ROOT_DIR}/bin/radixip-grpc-server" ]]; then
        run_suite "Rust" "${ROOT_DIR}/bin/radixip-grpc-server"
    fi
fi

# Run XDP Packet Drop Benchmarks
echo -e "\n${BOLD}${BLUE}========================================================================${NC}"
echo -e "${BOLD}${BLUE}  L2/L3 eBPF / XDP Packet Drop Benchmarks                               ${NC}"
echo -e "${BOLD}${BLUE}========================================================================${NC}"

if [[ "${TARGET}" == "all" || "${TARGET}" == "go" ]]; then
    echo -e "${BOLD}--- Go XDP Stack (Cilium eBPF + C xdp-c kernel obj) ---${NC}"
    run_xdp_packet_benchmark "go" "skb"
    run_xdp_packet_benchmark "go" "drv"
fi

if [[ "${TARGET}" == "all" || "${TARGET}" == "rust" ]]; then
    if [[ -f "${ROOT_DIR}/bin/rust-xdp-packet-bench" ]]; then
        echo -e "${BOLD}--- Rust XDP Stack (libbpf-rs + C xdp-c kernel obj) ---${NC}"
        run_xdp_packet_benchmark "rust" "skb"
        run_xdp_packet_benchmark "rust" "drv"
    fi
fi

# Print final comparison table
echo -e "\n${BOLD}${GREEN}========================================================================${NC}"
echo -e "${BOLD}${GREEN}  FINAL COMPARATIVE BENCHMARK SUMMARY                                   ${NC}"
echo -e "${BOLD}${GREEN}========================================================================${NC}"
printf "%-25s | %-20s | %-20s\n" "Enforcement Layer" "Go (Cilium/C eBPF)" "Rust (libbpf-rs/C eBPF)"
echo "--------------------------+----------------------+---------------------"
printf "%-25s | %-20s | %-20s\n" "1. Baseline (Allow)" "${RESULTS_BASELINE["Go"]:-N/A} req/s" "${RESULTS_BASELINE["Rust"]:-N/A} req/s"
printf "%-25s | %-20s | %-20s\n" "2. L7 Block (403)" "${RESULTS_L7["Go"]:-N/A} req/s" "${RESULTS_L7["Rust"]:-N/A} req/s"
printf "%-25s | %-20s | %-20s\n" "3. L2/3 XDP (Generic/SKB)" "${RESULTS_XDP_SKB["GO"]:-N/A}" "${RESULTS_XDP_SKB["RUST"]:-N/A}"
printf "%-25s | %-20s | %-20s\n" "4. L2/3 XDP (Native/DRV)" "${RESULTS_XDP_DRV["GO"]:-N/A}" "${RESULTS_XDP_DRV["RUST"]:-N/A}"
echo "------------------------------------------------------------------------"
echo "Note: All metrics are real measurements taken on isolated network namespaces."
echo -e "========================================================================\n"
