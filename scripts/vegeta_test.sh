#!/bin/bash
# Usage: ./vegeta_test.sh [rate] [duration] [blocklist_count]

set -e

RATE=${1:-50}  # Start with 50 to be safe
DURATION=${2:-15s}
BLOCKLIST_COUNT=${3:-10000}  # Smaller blocklist for testing

# Rate limiter config - make it generous
BURST=$((RATE * 5))
REFILL=$((RATE * 3))

echo "🚀 Load test: ${RATE} req/s for ${DURATION}"

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
cd scripts/seed_blocklist && go build -o ../../bin/seed_blocklist && cd ../..
echo -e "${GREEN}✅ Build complete${NC}"

# Kill everything
echo -e "${YELLOW}🔄 Cleaning up...${NC}"
pkill -f "testapp|spoof_proxy|seed_blocklist" 2>/dev/null || true
sleep 2

# Start testapp
echo -e "${YELLOW}🚀 Starting services...${NC}"
./bin/testapp -burst=${BURST} -refill=${REFILL} -ttl=60 -max-buckets=1000000 &
TESTAPP_PID=$!
sleep 2

# Check testapp
if ! curl -s http://localhost:8081/ping > /dev/null; then
    echo -e "${RED}❌ testapp failed${NC}"
    kill $TESTAPP_PID 2>/dev/null
    exit 1
fi
echo -e "${GREEN}✅ testapp running${NC}"

# Start spoof_proxy
./bin/spoof_proxy &
SPOOF_PID=$!
sleep 2

# Check spoof_proxy
if ! curl -s http://localhost:8082/health > /dev/null; then
    echo -e "${RED}❌ spoof_proxy failed${NC}"
    kill $TESTAPP_PID 2>/dev/null
    kill $SPOOF_PID 2>/dev/null
    exit 1
fi
echo -e "${GREEN}✅ spoof_proxy running${NC}"

# Test full path
if ! curl -s http://localhost:8080/ping > /dev/null; then
    echo -e "${RED}❌ proxy not working${NC}"
    kill $TESTAPP_PID 2>/dev/null
    kill $SPOOF_PID 2>/dev/null
    exit 1
fi
echo -e "${GREEN}✅ All services working${NC}"

# Seed blocklist (smaller for testing)
echo -e "${YELLOW}🌱 Seeding blocklist...${NC}"
./bin/seed_blocklist -count ${BLOCKLIST_COUNT} -addr localhost:8082
echo -e "${GREEN}✅ Blocklist seeded${NC}"

sleep 1

# Run test
echo -e "${YELLOW}📊 Running test...${NC}"
RESULT_FILE="results_${RATE}.txt"

echo "GET http://localhost:8080/ping" | \
    vegeta attack -rate=${RATE} -duration=${DURATION} -timeout=3s | \
    vegeta report -type=text > ${RESULT_FILE}

echo -e "\n${GREEN}=== Results ===${NC}"
cat ${RESULT_FILE}

# Check success rate
SUCCESS=$(grep "Success" ${RESULT_FILE} | grep -oP '\d+\.\d+%' | head -1)
echo -e "\n📊 Success rate: ${SUCCESS}"

# Cleanup
echo -e "${YELLOW}🧹 Cleaning up...${NC}"
kill $TESTAPP_PID 2>/dev/null
kill $SPOOF_PID 2>/dev/null

echo -e "${GREEN}✅ Done!${NC}"