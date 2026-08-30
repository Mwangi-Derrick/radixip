#!/bin/bash
# Usage: ./vegeta_test.sh [rate] [duration] [blocklist_count]
# Example: ./vegeta_test.sh 500 20s 100000

set -e

# Configuration
RATE=${1:-100}
DURATION=${2:-15s}
BLOCKLIST_COUNT=${3:-100000}

# Rate limiter config
BURST=$((RATE * 3))
REFILL=$((RATE * 2))

echo "🚀 Starting load test: ${RATE} req/s for ${DURATION}"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Check and install vegeta
if ! command -v vegeta &> /dev/null; then
    echo -e "${YELLOW}📦 Installing vegeta...${NC}"
    go install github.com/tsenart/vegeta@latest
    echo -e "${GREEN}✅ Vegeta installed${NC}"
fi

# Build everything
echo -e "${YELLOW}🔨 Building...${NC}"
cd scripts/testapp && go build -o ../../bin/testapp && cd ../..
cd scripts/spoof_proxy && go build -o ../../bin/spoof_proxy && cd ../..
cd scripts/seed_blocklist && go build -o ../../bin/seed_blocklist && cd ../..
echo -e "${GREEN}✅ Build complete${NC}"

# Cleanup old processes
echo -e "${YELLOW}🔄 Cleaning up...${NC}"
pkill -f "testapp|spoof_proxy|seed_blocklist" 2>/dev/null || true
sleep 2

# Start services
echo -e "${YELLOW}🚀 Starting services...${NC}"
./bin/testapp -burst=${BURST} -refill=${REFILL} -ttl=60 -max-buckets=1000000 &
TESTAPP_PID=$!
sleep 2

./bin/spoof_proxy &
SPOOF_PID=$!
sleep 2

# Check services
if ! curl -s http://localhost:8081/ping > /dev/null; then
    echo -e "${RED}❌ testapp failed${NC}"
    kill $TESTAPP_PID 2>/dev/null
    exit 1
fi

if ! curl -s http://localhost:8082/health > /dev/null; then
    echo -e "${RED}❌ spoof_proxy failed${NC}"
    kill $TESTAPP_PID 2>/dev/null
    kill $SPOOF_PID 2>/dev/null
    exit 1
fi

if ! curl -s http://localhost:8080/ping > /dev/null; then
    echo -e "${RED}❌ proxy not forwarding${NC}"
    kill $TESTAPP_PID 2>/dev/null
    kill $SPOOF_PID 2>/dev/null
    exit 1
fi

echo -e "${GREEN}✅ Services running${NC}"

# Seed blocklist
echo -e "${YELLOW}🌱 Seeding blocklist...${NC}"
./bin/seed_blocklist -count ${BLOCKLIST_COUNT} -addr localhost:8082
echo -e "${GREEN}✅ Blocklist seeded${NC}"

# Run load test
echo -e "${YELLOW}📊 Running load test...${NC}"
RESULT_FILE="load_test_results_${RATE}.txt"

echo "GET http://localhost:8080/ping" | \
    vegeta attack -rate=${RATE} -duration=${DURATION} -timeout=3s | \
    vegeta report -type=text > ${RESULT_FILE}

echo -e "\n${GREEN}=== Results ===${NC}"
cat ${RESULT_FILE}

# Cleanup
echo -e "${YELLOW}🧹 Cleaning up...${NC}"
kill $TESTAPP_PID 2>/dev/null
kill $SPOOF_PID 2>/dev/null

echo -e "${GREEN}✅ Done!${NC}"