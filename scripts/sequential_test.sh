#!/bin/bash
# Usage: ./sequential_test.sh
# Requires vegeta and jq installed

set -e

echo "🚀 Starting Sequential RadixIP Kitchen-Sink Validation Suite"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

if ! command -v vegeta &> /dev/null; then
    echo -e "${RED}❌ Please install vegeta: go install github.com/tsenart/vegeta@latest${NC}"
    exit 1
fi
if ! command -v jq &> /dev/null; then
    echo -e "${RED}❌ Please install jq${NC}"
    exit 1
fi

echo -e "${YELLOW}🔨 Building Kitchen Sink Apps...${NC}"
go build -o bin/kitchen-sink-go cmd/kitchen-sink-go/main.go
cargo build --bin kitchen-sink-rust --release
echo -e "${GREEN}✅ Build complete${NC}"

echo -e "${YELLOW}🔄 Cleaning up...${NC}"
pkill -f "kitchen-sink-go|kitchen-sink-rust" 2>/dev/null || true
sleep 2

echo -e "${YELLOW}🚀 Starting Go Kitchen Sink App...${NC}"
./bin/kitchen-sink-go &
GO_PID=$!

echo -e "${YELLOW}🚀 Starting Rust Kitchen Sink App...${NC}"
./target/release/kitchen-sink-rust &
RUST_PID=$!

sleep 3 # Wait for servers to spin up

TARGETS=(
    "Gin (Go):8081"
    "Echo (Go):8082"
    "Fiber (Go):8083"
    "Axum (Rust):9081"
    "Actix (Rust):9082"
)

echo -e "\n${GREEN}==========================================${NC}"
echo -e "${GREEN} Phase 1: Route-Trie Specific Rate Limits ${NC}"
echo -e "${GREEN}==========================================${NC}"
# The config has default limits. Let's say /api/v1/auth has a lower limit than /api/v1/public.

for target in "${TARGETS[@]}"; do
    IFS=":" read -r name port <<< "$target"
    echo -e "\n${YELLOW}Testing $name on port $port...${NC}"
    
    echo "GET http://localhost:$port/api/v1/auth" > target_auth.txt
    echo "GET http://localhost:$port/api/v1/public" > target_public.txt
    
    echo -e "Attacking /api/v1/auth at 1000 RPS for 2s (Expect high 429 rate)"
    vegeta attack -rate=1000 -duration=2s -targets=target_auth.txt | vegeta report -type=json > result_auth.json
    
    echo -e "Attacking /api/v1/public at 1000 RPS for 2s (Expect lower 429 rate or 200s)"
    vegeta attack -rate=1000 -duration=2s -targets=target_public.txt | vegeta report -type=json > result_public.json
    
    auth_success=$(jq '.success' result_auth.json)
    public_success=$(jq '.success' result_public.json)
    echo -e "Auth Success Rate: $auth_success"
    echo -e "Public Success Rate: $public_success"
done

echo -e "\n${GREEN}==========================================${NC}"
echo -e "${GREEN} Phase 2: Auto-Ban Trigger & Sweeper Test ${NC}"
echo -e "${GREEN}==========================================${NC}"

# Attack Gin continuously to trigger auto-ban across the shared Engine
echo -e "${YELLOW}Attacking Gin (Port 8081) at 5000 RPS for 5s to trigger Auto-Ban...${NC}"
echo "GET http://localhost:8081/api/v1/public" | vegeta attack -rate=5000 -duration=5s | vegeta report -type=json > result_autoban.json

status_429=$(jq '.status_codes["429"] // 0' result_autoban.json)
status_403=$(jq '.status_codes["403"] // 0' result_autoban.json)

echo -e "429 Responses (Rate Limited): $status_429"
echo -e "403 Responses (Auto-Banned): $status_403"

if [ "$status_403" -gt 0 ]; then
    echo -e "${GREEN}✅ Auto-Ban triggered successfully!${NC}"
else
    echo -e "${RED}❌ Auto-Ban failed to trigger.${NC}"
fi

echo -e "\n${YELLOW}Waiting 35 seconds for Sweeper to remove the ban...${NC}"
sleep 35

echo -e "Sending single request to Gin (Port 8081) to verify ban lifted..."
STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8081/api/v1/public)
if [ "$STATUS" -eq 200 ]; then
    echo -e "${GREEN}✅ Sweeper successfully lifted the ban (Status 200 OK)!${NC}"
else
    echo -e "${RED}❌ IP is still blocked or limited (Status $STATUS)!${NC}"
fi

echo -e "\n${YELLOW}🧹 Cleaning up...${NC}"
kill $GO_PID 2>/dev/null
kill $RUST_PID 2>/dev/null

rm -f target_auth.txt target_public.txt result_auth.json result_public.json result_autoban.json

echo -e "${GREEN}✅ All Tests Completed!${NC}"
