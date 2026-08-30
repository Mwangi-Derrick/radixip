#!/bin/bash
# Usage: ./vegeta_test.sh [rate] [duration] [blocklist_count]
# Example: ./vegeta_test.sh 500 20s 100000

set -e  # Exit on error

# Configuration with defaults
RATE=${1:-100}
DURATION=${2:-15s}
BLOCKLIST_COUNT=${3:-100000}

echo "🚀 Starting local load test..."
echo "📊 Configuration: ${RATE} req/s for ${DURATION} with ${BLOCKLIST_COUNT} CIDRs"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

# Check if vegeta is installed
if ! command -v vegeta &> /dev/null; then
    echo -e "${YELLOW}📦 Vegeta not found. Installing...${NC}"
    go install github.com/tsenart/vegeta@latest
    echo -e "${GREEN}✅ Vegeta installed${NC}"
fi

# Build all components
echo -e "${YELLOW}🔨 Building components...${NC}"
cd scripts/testapp && go build -o ../../bin/testapp && cd ../..
cd scripts/spoof_proxy && go build -o ../../bin/spoof_proxy && cd ../..
cd scripts/seed_blocklist && go build -o ../../bin/seed_blocklist && cd ../..
echo -e "${GREEN}✅ Build complete${NC}"

# Kill any existing processes
echo -e "${YELLOW}🔄 Cleaning up old processes...${NC}"
pkill -f testapp 2>/dev/null || true
pkill -f spoof_proxy 2>/dev/null || true
sleep 1

# Start services
echo -e "${YELLOW}🚀 Starting services...${NC}"
./bin/testapp &
TESTAPP_PID=$!
sleep 2

./bin/spoof_proxy &
SPOOF_PID=$!
sleep 2

# Check if services are running
if ! curl -s http://localhost:8080/ping > /dev/null; then
    echo -e "${RED}❌ testapp not responding on port 8080${NC}"
    kill $TESTAPP_PID 2>/dev/null
    kill $SPOOF_PID 2>/dev/null
    exit 1
fi

if ! curl -s http://localhost:8082/health > /dev/null; then
    echo -e "${RED}❌ spoof_proxy not responding on port 8082${NC}"
    kill $TESTAPP_PID 2>/dev/null
    kill $SPOOF_PID 2>/dev/null
    exit 1
fi

echo -e "${GREEN}✅ Services running${NC}"

# Seed blocklist with configurable count
echo -e "${YELLOW}🌱 Seeding blocklist with ${BLOCKLIST_COUNT} CIDRs...${NC}"
./bin/seed_blocklist -count ${BLOCKLIST_COUNT} -addr localhost:8082
echo -e "${GREEN}✅ Blocklist seeded${NC}"

# Run load test with configurable rate and duration
echo -e "${YELLOW}📊 Running load test (${RATE} req/s for ${DURATION})...${NC}"
RESULT_FILE="load_test_results_${RATE}_${DURATION}.txt"
echo "GET http://localhost:8080/ping" | \
    vegeta attack -rate=${RATE} -duration=${DURATION} -timeout=5s | \
    vegeta report -type=text | tee ${RESULT_FILE}

# Parse results
echo ""
echo -e "${GREEN}=== Load Test Results ===${NC}"
cat ${RESULT_FILE}

# Extract metrics
SUCCESS=$(grep "Success" ${RESULT_FILE} | grep -oP '\d+\.\d+%' | head -1)
P99=$(grep "99th" ${RESULT_FILE} | grep -oP '[\d.]+(ms|s)' | head -1)
MEAN=$(grep "mean" ${RESULT_FILE} | grep -oP '[\d.]+(ms|s)' | head -1)
REQUESTS=$(grep "Requests" ${RESULT_FILE} | grep -oP '\[\d+\]' | grep -oP '\d+')

echo -e "\n📊 Summary:"
echo "   Requests total: ${REQUESTS}"
echo "   Success rate: ${SUCCESS}"
echo "   Mean latency: ${MEAN}"
echo "   p99 latency: ${P99}"

# Check p99 threshold (100ms)
THRESHOLD_FAIL=0
if [[ $P99 == *"ms"* ]]; then
    VALUE=$(echo $P99 | sed 's/ms//' | tr -d '[:space:]')
    if (( $(echo "$VALUE > 100" | bc -l 2>/dev/null || echo "0") )); then
        echo -e "${RED}❌ p99 latency ($VALUE ms) exceeds 100ms threshold${NC}"
        THRESHOLD_FAIL=1
    else
        echo -e "${GREEN}✅ p99 latency ($VALUE ms) within acceptable range${NC}"
    fi
elif [[ $P99 == *"s"* ]]; then
    VALUE=$(echo $P99 | sed 's/s//' | awk '{print $1 * 1000}')
    if (( $(echo "$VALUE > 100" | bc -l 2>/dev/null || echo "0") )); then
        echo -e "${RED}❌ p99 latency ($VALUE ms) exceeds 100ms threshold${NC}"
        THRESHOLD_FAIL=1
    else
        echo -e "${GREEN}✅ p99 latency ($VALUE ms) within acceptable range${NC}"
    fi
else
    echo -e "${YELLOW}⚠️  Could not parse p99 value: $P99${NC}"
fi

# Save results to a summary file
SUMMARY_FILE="test_summary_${RATE}_${DURATION}.txt"
cat > ${SUMMARY_FILE} << EOF
=== Load Test Summary ===
Date: $(date)
Rate: ${RATE} req/s
Duration: ${DURATION}
Blocklist: ${BLOCKLIST_COUNT} CIDRs
Requests total: ${REQUESTS}
Success rate: ${SUCCESS}
Mean latency: ${MEAN}
p99 latency: ${P99}
Threshold: ${THRESHOLD_FAIL}
EOF

echo -e "\n📝 Results saved to: ${RESULT_FILE} and ${SUMMARY_FILE}"

# Cleanup
echo -e "${YELLOW}🧹 Cleaning up...${NC}"
kill $TESTAPP_PID 2>/dev/null
kill $SPOOF_PID 2>/dev/null

if [ "$THRESHOLD_FAIL" = "1" ]; then
    echo -e "${RED}❌ Load test failed due to performance threshold${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Load test complete!${NC}"