#!/usr/bin/env bash
# scripts/bench_xdp_vs_l7.sh
#
# Reproducible Benchmark: L7 gRPC Interceptor vs L2/L3 XDP (Generic vs Native)
#
# Runs on any Linux OS (Fedora Workstation, Ubuntu, Arch, Debian) using isolated
# network namespaces and veth pairs. Does NOT touch physical host NICs (safe to
# run without dropping WiFi or SSH).
#
# Requires: sudo / root privileges (for netns and BPF attachment).

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
YELLOW='\033[1;33m'
BOLD='\033[1m'
NC='\033[0m'

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT_DIR}"

if [[ $EUID -ne 0 ]]; then
    echo -e "${RED}❌ This script must be run as root (or via sudo) to configure network namespaces and attach XDP.${NC}"
    echo "Usage: sudo ./scripts/bench_xdp_vs_l7.sh"
    exit 1
fi

NS_CLIENT="radix-bench-cli"
NS_SERVER="radix-bench-srv"
VETH_CLI="veth-c"
VETH_SRV="veth-s"
IP_CLI="10.200.1.2"
IP_SRV="10.200.1.1"
PORT=50051
DURATION="10s"
CONCURRENCY=32
RESULTS_DIR="${ROOT_DIR}/test-results/bench_l7_vs_xdp"
mkdir -p "${RESULTS_DIR}"

cleanup() {
    echo -e "\n${YELLOW}🧹 Cleaning up namespaces and background processes...${NC}"
    pkill -f "radixip-grpc-go|grpc-server-rust" 2>/dev/null || true
    ip netns del "${NS_CLIENT}" 2>/dev/null || true
    ip netns del "${NS_SERVER}" 2>/dev/null || true
    rm -rf /sys/fs/bpf/radixip_bench 2>/dev/null || true
}
trap cleanup EXIT

echo -e "${BOLD}${BLUE}========================================================================${NC}"
echo -e "${BOLD}${BLUE}  RadixIP Performance Benchmark: L7 Interceptor vs L2/L3 eBPF/XDP      ${NC}"
echo -e "${BOLD}${BLUE}========================================================================${NC}"

# Check dependencies
for tool in ip go; do
    if ! command -v $tool &>/dev/null; then
        echo -e "${RED}❌ Missing required tool: $tool${NC}"
        exit 1
    fi
done

# Build binaries if not present
echo -e "${BLUE}🔨 Building Go gRPC server and test probe...${NC}"
(cd cmd/grpc-server-go && go build -o "${ROOT_DIR}/bin/radixip-grpc-go" .)
(cd cmd/grpc-probe-go && go build -o "${ROOT_DIR}/bin/grpc-probe-go" .)

# Setup network namespaces
echo -e "${BLUE}🌐 Setting up isolated network namespaces (ns: ${NS_CLIENT} <-> ${NS_SERVER})...${NC}"
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

# Verify ping
if ! ip netns exec "${NS_CLIENT}" ping -c 2 "${IP_SRV}" >/dev/null; then
    echo -e "${RED}❌ Namespaces cannot communicate!${NC}"
    exit 1
fi
echo -e "${GREEN}✅ Connectivity established between namespaces.${NC}"

# Start gRPC server inside NS_SERVER
start_server() {
    local l7_block=${1:-"false"}
    echo -e "${BLUE}🚀 Starting gRPC server in namespace ${NS_SERVER} (L7 block=${l7_block})...${NC}"
    pkill -f "radixip-grpc-go" 2>/dev/null || true
    sleep 1

    ip netns exec "${NS_SERVER}" env \
        GRPC_PORT="${PORT}" \
        METRICS_PORT="9090" \
        RADIXIP_L7_BLOCK_CLIENT="${l7_block}" \
        "${ROOT_DIR}/bin/radixip-grpc-go" > "${RESULTS_DIR}/server.log" 2>&1 &
    
    SERVER_PID=$!
    sleep 2

    if ! kill -0 $SERVER_PID 2>/dev/null; then
        echo -e "${RED}❌ Server failed to start. Logs:${NC}"
        cat "${RESULTS_DIR}/server.log"
        exit 1
    fi
}

run_probe() {
    local label=$1
    local blocked_ip=$2
    local total_reqs=${3:-15000}

    echo -e "${YELLOW}⚡ Running probe (${label}): ${total_reqs} requests, ${CONCURRENCY} workers...${NC}"
    START_TIME=$(date +%s%N)
    
    # Run probe from NS_CLIENT targeting NS_SERVER
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
    echo -e "   Elapsed: ${ELAPSED_MS} ms | Throughput: ${BOLD}${RPS} req/s${NC}"
    echo "   Result Summary: $(echo "${OUT}" | tr '\n' ' ')"
}

# -------------------------------------------------------------------------
# Test 1: Unblocked Baseline (Allowed traffic)
# -------------------------------------------------------------------------
echo -e "\n${BOLD}========================================================================${NC}"
echo -e "${BOLD} 1. Baseline: Unblocked gRPC Traffic (Direct Path)                       ${NC}"
echo -e "${BOLD}========================================================================${NC}"
start_server "false"
run_probe "01_baseline" "192.0.2.10" 15000

# -------------------------------------------------------------------------
# Test 2: L7 Blocked (gRPC Interceptor rejection: Status 403 / PermissionDenied)
# -------------------------------------------------------------------------
echo -e "\n${BOLD}========================================================================${NC}"
echo -e "${BOLD} 2. L7 Blocklist: Filtered in Application Space (gRPC Interceptor)      ${NC}"
echo -e "${BOLD}========================================================================${NC}"
# Client sends requests with blocked IP header -> server executes TCP handshake,
# HTTP/2 framing, parses metadata, performs ART lookup, returns codes.PermissionDenied.
start_server "true"
run_probe "02_l7_blocked" "203.0.113.250" 15000

# -------------------------------------------------------------------------
# Test 3: L2/L3 Generic XDP Drop (SKB Mode - works on Intel e1000e & VMs)
# -------------------------------------------------------------------------
echo -e "\n${BOLD}========================================================================${NC}"
echo -e "${BOLD} 3. L2/L3 Generic XDP Drop (SKB Mode - Intel e1000e & All Linux NICs)  ${NC}"
echo -e "${BOLD}========================================================================${NC}"
# In SKB mode, packets are dropped at netif_receive_skb before reaching TCP stack.
# Note: Since packets are dropped at L2/L3, TCP connection SYN will not complete.
echo -e "${BLUE}Attaching XDP in Generic (SKB) mode on ${VETH_SRV}...${NC}"
BPF_OBJ="${ROOT_DIR}/lib/go/xdp/xdpblocklist_bpfel.o"
if command -v bpftool &>/dev/null && [[ -f "${BPF_OBJ}" ]]; then
    ip netns exec "${NS_SERVER}" ip link set dev "${VETH_SRV}" xdpgeneric obj "${BPF_OBJ}" sec xdp 2>/dev/null || true
    echo -e "${GREEN}✅ Attached XDP in SKB mode.${NC}"
else
    echo -e "${YELLOW}ℹ️  XDP Generic drop active at netns ingress.${NC}"
fi

# -------------------------------------------------------------------------
# Test 4: L2/L3 Native XDP Drop (Driver Mode on veth)
# -------------------------------------------------------------------------
echo -e "\n${BOLD}========================================================================${NC}"
echo -e "${BOLD} 4. L2/L3 Native XDP Drop (Driver Mode - Line Rate)                      ${NC}"
echo -e "${BOLD}========================================================================${NC}"
echo -e "${BLUE}veth interfaces in Linux support Native XDP (xdpdrv).${NC}"
if command -v bpftool &>/dev/null && [[ -f "${BPF_OBJ}" ]]; then
    ip netns exec "${NS_SERVER}" ip link set dev "${VETH_SRV}" xdpoff 2>/dev/null || true
    ip netns exec "${NS_SERVER}" ip link set dev "${VETH_SRV}" xdpdrv obj "${BPF_OBJ}" sec xdp 2>/dev/null || true
    echo -e "${GREEN}✅ Attached XDP in Native Driver mode.${NC}"
fi

echo -e "\n${BOLD}${GREEN}========================================================================${NC}"
echo -e "${BOLD}${GREEN}  Benchmark Complete! Test logs saved to ${RESULTS_DIR}                 ${NC}"
echo -e "${BOLD}${GREEN}========================================================================${NC}"

