#!/bin/bash
# blocklist_test.sh - simulates an attacker from a blocked CIDR to prove blocklist
# Usage: ./blocklist_test.sh

set -e

RATE=100
DURATION=5s
FIXED_IP="198.51.100.99"
BLOCKED_CIDR="198.51.100.0/24"

echo "🚀 Blocklist Load test: ${RATE} req/s for ${DURATION}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Install vegeta if needed
if ! command -v vegeta &> /dev/null; then
    echo -e "${YELLOW}📦 Installing vegeta...${NC}"
    go install github.com/tsenart/vegeta@latest
fi

# Build
echo -e "${YELLOW}🔨 Building...${NC}"
cd scripts/testapp && go build -o ../../bin/testapp && cd ../..
cd scripts/spoof_proxy && go build -o ../../bin/spoof_proxy && cd ../..
echo -e "${GREEN}✅ Build complete${NC}"

# Kill everything
echo -e "${YELLOW}🔄 Cleaning up...${NC}"
pkill -f "testapp|spoof_proxy" 2>/dev/null || true
sleep 2

# Start testapp
echo -e "${YELLOW}🚀 Starting testapp...${NC}"
./bin/testapp -burst=1000 -refill=1000 -ttl=60 -max-buckets=10000 > /dev/null 2>&1 &
TESTAPP_PID=$!
sleep 2

# Check testapp
if ! curl -s http://localhost:8081/ping > /dev/null; then
    echo -e "${RED}❌ testapp failed${NC}"
    kill $TESTAPP_PID 2>/dev/null
    exit 1
fi
echo -e "${GREEN}✅ testapp running${NC}"

# Start spoof_proxy with fixed IP
./bin/spoof_proxy --fixed-ip ${FIXED_IP} > /dev/null 2>&1 &
SPOOF_PID=$!
sleep 2

# Check spoof_proxy
if ! curl -s http://localhost:8082/health > /dev/null; then
    echo -e "${RED}❌ spoof_proxy failed${NC}"
    kill $TESTAPP_PID 2>/dev/null
    kill $SPOOF_PID 2>/dev/null
    exit 1
fi
echo -e "${GREEN}✅ spoof_proxy running with IP ${FIXED_IP}${NC}"

# Seed the specific blocklist CIDR
echo -e "${YELLOW}🌱 Seeding blocklist with ${BLOCKED_CIDR}...${NC}"
curl -s -X POST http://localhost:8082/seed -H "Content-Type: application/json" -d "{\"cidrs\":[\"${BLOCKED_CIDR}\"]}" > /dev/null
echo -e "${GREEN}✅ Blocklist seeded${NC}"

sleep 1

# Run test
echo -e "${YELLOW}📊 Running blocklist simulation...${NC}"
RESULT_FILE="results_blocklist.txt"

echo "GET http://localhost:8080/ping" | \
    vegeta attack -rate=${RATE} -duration=${DURATION} -timeout=3s | \
    vegeta report -type=text > ${RESULT_FILE}

echo -e "\n${GREEN}=== Results ===${NC}"
cat ${RESULT_FILE}

# Check success rate (should be 0%)
SUCCESS_PERCENT=$(grep "Success" ${RESULT_FILE} | grep -oP '\d+\.\d+' | head -1)

if awk -v success="$SUCCESS_PERCENT" 'BEGIN { if (success == 0.0) exit 0; else exit 1 }'; then
    echo -e "\n${GREEN}✅ Blocklist PROOF: Success rate is ${SUCCESS_PERCENT}%, attacker was fully blocked!${NC}"
else
    echo -e "\n${RED}❌ Blocklist FAILED: Success rate is ${SUCCESS_PERCENT}%, attacker slipped through!${NC}"
    kill $TESTAPP_PID 2>/dev/null
    kill $SPOOF_PID 2>/dev/null
    exit 1
fi

STATUS_CODES=$(grep "Status Codes" ${RESULT_FILE})
if echo "$STATUS_CODES" | grep -q "403"; then
    echo -e "${GREEN}✅ Blocklist PROOF: 403 Forbidden seen in responses!${NC}"
else
    echo -e "${RED}❌ Blocklist FAILED: No 403 status codes found!${NC}"
    kill $TESTAPP_PID 2>/dev/null
    kill $SPOOF_PID 2>/dev/null
    exit 1
fi

# Cleanup
echo -e "${YELLOW}🧹 Cleaning up...${NC}"
kill $TESTAPP_PID 2>/dev/null
kill $SPOOF_PID 2>/dev/null

echo -e "${GREEN}✅ Done!${NC}"
