#!/bin/bash
# attacker_test.sh - simulates an attacker flooding from a single IP to prove rate limiting
# Usage: ./attacker_test.sh

set -e

RATE=500
DURATION=10s
FIXED_IP="203.0.113.42"

# Rate limiter config - strict
BURST=100
REFILL=10

echo "🚀 Single-IP Attacker Load test: ${RATE} req/s for ${DURATION}"

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
./bin/testapp -burst=${BURST} -refill=${REFILL} -ttl=60 -max-buckets=10000 > /dev/null 2>&1 &
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

sleep 1

# Run test
echo -e "${YELLOW}📊 Running attack simulation...${NC}"
RESULT_FILE="results_attacker.txt"

echo "GET http://localhost:8080/ping" | \
    vegeta attack -rate=${RATE} -duration=${DURATION} -timeout=3s | \
    vegeta report -type=text > ${RESULT_FILE}

echo -e "\n${GREEN}=== Results ===${NC}"
cat ${RESULT_FILE}

# Check success rate (should be very low because rate limiter kicked in)
SUCCESS_PERCENT=$(grep "Success" ${RESULT_FILE} | grep -oP '\d+\.\d+' | head -1)

# In 10 seconds at 500 req/s = 5000 requests.
# Burst 100 + (10 * 10 refill) = ~200 allowed.
# 200 / 5000 = 4% success rate.
# So if success rate is < 10%, rate limiting worked.
if (( $(echo "$SUCCESS_PERCENT < 10.0" | bc -l) )); then
    echo -e "\n${GREEN}✅ Rate Limiter PROOF: Success rate is ${SUCCESS_PERCENT}%, attacker was blocked!${NC}"
else
    echo -e "\n${RED}❌ Rate Limiter FAILED: Success rate is ${SUCCESS_PERCENT}%, attacker slipped through!${NC}"
    kill $TESTAPP_PID 2>/dev/null
    kill $SPOOF_PID 2>/dev/null
    exit 1
fi

STATUS_CODES=$(grep "Status Codes" ${RESULT_FILE})
if echo "$STATUS_CODES" | grep -q "429"; then
    echo -e "${GREEN}✅ Rate Limiter PROOF: 429 Too Many Requests seen in responses!${NC}"
else
    echo -e "${RED}❌ Rate Limiter FAILED: No 429 status codes found!${NC}"
    kill $TESTAPP_PID 2>/dev/null
    kill $SPOOF_PID 2>/dev/null
    exit 1
fi

# Cleanup
echo -e "${YELLOW}🧹 Cleaning up...${NC}"
kill $TESTAPP_PID 2>/dev/null
kill $SPOOF_PID 2>/dev/null

echo -e "${GREEN}✅ Done!${NC}"
