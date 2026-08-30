#!/bin/bash
# Usage: ./vegeta_test.sh [rate] [duration] [blocklist_count]
# Example: ./vegeta_test.sh 500 20s 100000

set -e

# Configuration with defaults
RATE=${1:-100}
DURATION=${2:-15s}
BLOCKLIST_COUNT=${3:-100000}

# Rate limiter configuration (integer math)
BURST=$((RATE * 2))
REFILL=$((RATE * 3 / 2))

# Ensure minimum values
[ "$REFILL" -lt 10 ] && REFILL=10
[ "$BURST" -lt 20 ] && BURST=20

echo "🚀 Starting local load test..."
echo "📊 Configuration: ${RATE} req/s for ${DURATION} with ${BLOCKLIST_COUNT} CIDRs"
echo "🔧 Rate Limiter: burst=${BURST}, refill=${REFILL}/s"

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

# Check vegeta
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

# Cleanup
echo -e "${YELLOW}🔄 Cleaning up...${NC}"
pkill -f "testapp|spoof_proxy|seed_blocklist" 2>/dev/null || true
sleep 2

# Start services
echo -e "${YELLOW}🚀 Starting services...${NC}"

# Start testapp with flags
./bin/testapp -burst=${BURST} -refill=${REFILL} -ttl=60 -max-buckets=1000000 &
TESTAPP_PID=$!
sleep 2

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

if ! curl -s http://localhost:8082/health > /dev/null; then
    echo -e "${RED}❌ spoof_proxy failed${NC}"
    kill $TESTAPP_PID 2>/dev/null
    kill $SPOOF_PID 2>/dev/null
    exit 1
fi
echo -e "${GREEN}✅ spoof_proxy running${NC}"

# Check full path
if ! curl -s http://localhost:8080/ping > /dev/null; then
    echo -e "${RED}❌ proxy not forwarding${NC}"
    kill $TESTAPP_PID 2>/dev/null
    kill $SPOOF_PID 2>/dev/null
    exit 1
fi
echo -e "${GREEN}✅ All services running${NC}"

# Seed blocklist
echo -e "${YELLOW}🌱 Seeding blocklist...${NC}"
./bin/seed_blocklist -count ${BLOCKLIST_COUNT} -addr localhost:8082
echo -e "${GREEN}✅ Blocklist seeded${NC}"

sleep 1

# Run load test
echo -e "${YELLOW}📊 Running load test...${NC}"
RESULT_FILE="load_test_results_${RATE}_${DURATION}.txt"

echo "GET http://localhost:8080/ping" | \
    vegeta attack -rate=${RATE} -duration=${DURATION} -timeout=5s | \
    vegeta report -type=text > ${RESULT_FILE}

# Display results
echo -e "\n${GREEN}=== Results ===${NC}"
cat ${RESULT_FILE}

# Parse metrics
SUCCESS=$(grep "Success" ${RESULT_FILE} | sed -n 's/.*\([0-9]*\.[0-9]*%\).*/\1/p' | head -1)
P99=$(grep "99th" ${RESULT_FILE} | sed -n 's/.*\([0-9]*\.[0-9]*ms\).*/\1/p' | head -1)
MEAN=$(grep "mean" ${RESULT_FILE} | sed -n 's/.*\([0-9]*\.[0-9]*ms\).*/\1/p' | head -1)

echo -e "\n📊 Summary:"
echo "   Target rate: ${RATE} req/s"
echo "   Success rate: ${SUCCESS:-N/A}"
echo "   Mean latency: ${MEAN:-N/A}"
echo "   p99 latency: ${P99:-N/A}"

# Cleanup
echo -e "${YELLOW}🧹 Cleaning up...${NC}"
kill $TESTAPP_PID 2>/dev/null || true
kill $SPOOF_PID 2>/dev/null || true

echo -e "${GREEN}✅ Load test complete!${NC}"