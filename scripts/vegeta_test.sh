#!/bin/bash
# Usage: ./vegeta_test.sh [rate] [duration] [blocklist_count]
# Example: ./vegeta_test.sh 500 20s 100000

set -e  # Exit on error

# Configuration with defaults
RATE=${1:-100}
DURATION=${2:-15s}
BLOCKLIST_COUNT=${3:-100000}

# Rate limiter configuration (adjust based on test rate)
# Set burst to 2x the target rate, refill to 1.5x target rate
BURST=$((RATE * 2))
REFILL=$(echo "scale=0; $RATE * 1.5" | bc | cut -d. -f1)
if [ "$REFILL" -lt 100 ]; then
    REFILL=100
fi

echo "🚀 Starting local load test..."
echo "📊 Configuration: ${RATE} req/s for ${DURATION} with ${BLOCKLIST_COUNT} CIDRs"
echo "🔧 Rate Limiter: burst=${BURST}, refill=${REFILL}/s"

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

# Build all components with rate limit flags
echo -e "${YELLOW}🔨 Building components...${NC}"

# Build testapp with configurable rate limits
cd scripts/testapp && \
    go build -o ../../bin/testapp \
    -ldflags="-X main.burst=${BURST} -X main.refill=${REFILL}" \
    . && \
    cd ../..

cd scripts/spoof_proxy && go build -o ../../bin/spoof_proxy && cd ../..
cd scripts/seed_blocklist && go build -o ../../bin/seed_blocklist && cd ../..
echo -e "${GREEN}✅ Build complete${NC}"

# AGGRESSIVE CLEANUP
echo -e "${YELLOW}🔄 Cleaning up old processes...${NC}"

# Kill by port (Linux/Mac)
if [[ "$OSTYPE" == "linux-gnu"* ]] || [[ "$OSTYPE" == "darwin"* ]]; then
    fuser -k 8080/tcp 2>/dev/null || true
    fuser -k 8081/tcp 2>/dev/null || true
    fuser -k 8082/tcp 2>/dev/null || true
fi

# Kill by process name
pkill -f testapp 2>/dev/null || true
pkill -f spoof_proxy 2>/dev/null || true
pkill -f seed_blocklist 2>/dev/null || true

sleep 2

# Start services with rate limit flags
echo -e "${YELLOW}🚀 Starting services...${NC}"

# Start testapp with rate limit configuration
./bin/testapp -burst=${BURST} -refill=${REFILL} -ttl=60 -max-buckets=1000000 &
TESTAPP_PID=$!
echo -e "   Testapp PID: ${TESTAPP_PID}"

# Wait for testapp to start
MAX_RETRIES=10
RETRY=0
while ! curl -s http://localhost:8081/ping > /dev/null && [ $RETRY -lt $MAX_RETRIES ]; do
    sleep 1
    RETRY=$((RETRY+1))
done

if [ $RETRY -ge $MAX_RETRIES ]; then
    echo -e "${RED}❌ testapp failed to start${NC}"
    kill $TESTAPP_PID 2>/dev/null
    exit 1
fi
echo -e "${GREEN}✅ testapp running on :8081${NC}"

# Start spoof_proxy
./bin/spoof_proxy &
SPOOF_PID=$!
echo -e "   Spoof PID: ${SPOOF_PID}"

# Wait for spoof_proxy
RETRY=0
while ! curl -s http://localhost:8082/health > /dev/null && [ $RETRY -lt $MAX_RETRIES ]; do
    sleep 1
    RETRY=$((RETRY+1))
done

if [ $RETRY -ge $MAX_RETRIES ]; then
    echo -e "${RED}❌ spoof_proxy failed to start${NC}"
    kill $TESTAPP_PID 2>/dev/null
    kill $SPOOF_PID 2>/dev/null
    exit 1
fi
echo -e "${GREEN}✅ spoof_proxy running on :8082${NC}"

# Check if testapp is reachable through spoof_proxy
if ! curl -s http://localhost:8080/ping > /dev/null; then
    echo -e "${RED}❌ spoof_proxy not forwarding to testapp${NC}"
    kill $TESTAPP_PID 2>/dev/null
    kill $SPOOF_PID 2>/dev/null
    exit 1
fi

echo -e "${GREEN}✅ All services running${NC}"

# Seed blocklist
echo -e "${YELLOW}🌱 Seeding blocklist with ${BLOCKLIST_COUNT} CIDRs...${NC}"
./bin/seed_blocklist -count ${BLOCKLIST_COUNT} -addr localhost:8082
echo -e "${GREEN}✅ Blocklist seeded${NC}"

sleep 1

# Run load test
echo -e "${YELLOW}📊 Running load test (${RATE} req/s for ${DURATION})...${NC}"
RESULT_FILE="load_test_results_${RATE}_${DURATION}.txt"

echo "GET http://localhost:8080/ping" | \
    vegeta attack -rate=${RATE} -duration=${DURATION} -timeout=5s | \
    vegeta report -type=text > ${RESULT_FILE}

# Parse and display results
echo ""
echo -e "${GREEN}=== Load Test Results ===${NC}"
cat ${RESULT_FILE}

# Extract metrics
SUCCESS=$(grep "Success" ${RESULT_FILE} | grep -oP '\d+\.\d+%' | head -1)
P99=$(grep "99th" ${RESULT_FILE} | grep -oP '[\d.]+(ms|s)' | head -1)
MEAN=$(grep "mean" ${RESULT_FILE} | grep -oP '[\d.]+(ms|s)' | head -1)
REQUESTS=$(grep "Requests" ${RESULT_FILE} | grep -oP '\[\d+\]' | grep -oP '\d+' | head -1)
THROUGHPUT=$(grep "throughput" ${RESULT_FILE} | grep -oP '\d+\.\d+' | head -1)
RATE_ACTUAL=$(grep "rate" ${RESULT_FILE} | grep -oP '\d+\.\d+' | head -1)

echo -e "\n📊 Summary:"
echo "   Target rate: ${RATE} req/s"
echo "   Actual rate: ${RATE_ACTUAL:-N/A} req/s"
echo "   Throughput: ${THROUGHPUT:-N/A} req/s"
echo "   Requests total: ${REQUESTS:-N/A}"
echo "   Success rate: ${SUCCESS:-N/A}"
echo "   Mean latency: ${MEAN:-N/A}"
echo "   p99 latency: ${P99:-N/A}"

# Check success rate
SUCCESS_VALUE=$(echo $SUCCESS | sed 's/%//' | tr -d '[:space:]')
if [[ -n "$SUCCESS_VALUE" ]]; then
    if (( $(echo "$SUCCESS_VALUE < 90" | bc -l 2>/dev/null || echo "0") )); then
        echo -e "${RED}❌ Success rate (${SUCCESS}) is below 90%${NC}"
        THRESHOLD_FAIL=1
    else
        echo -e "${GREEN}✅ Success rate (${SUCCESS}) is acceptable${NC}"
    fi
fi

# Check p99 threshold
if [[ -n "$P99" ]]; then
    if [[ $P99 == *"ms"* ]]; then
        VALUE=$(echo $P99 | sed 's/ms//' | tr -d '[:space:]')
        if (( $(echo "$VALUE > 100" | bc -l 2>/dev/null || echo "0") )); then
            echo -e "${RED}❌ p99 latency ($VALUE ms) exceeds 100ms threshold${NC}"
            THRESHOLD_FAIL=1
        else
            echo -e "${GREEN}✅ p99 latency ($VALUE ms) within acceptable range${NC}"
        fi
    fi
fi

# Save summary
SUMMARY_FILE="test_summary_${RATE}_${DURATION}.txt"
cat > ${SUMMARY_FILE} << EOF
=== Load Test Summary ===
Date: $(date)
Rate: ${RATE} req/s
Duration: ${DURATION}
Blocklist: ${BLOCKLIST_COUNT} CIDRs
Rate Limiter: burst=${BURST}, refill=${REFILL}/s
Actual Rate: ${RATE_ACTUAL:-N/A} req/s
Throughput: ${THROUGHPUT:-N/A} req/s
Requests total: ${REQUESTS:-N/A}
Success rate: ${SUCCESS:-N/A}
Mean latency: ${MEAN:-N/A}
p99 latency: ${P99:-N/A}
Threshold: ${THRESHOLD_FAIL}
EOF

echo -e "\n📝 Results saved to: ${RESULT_FILE} and ${SUMMARY_FILE}"

# Cleanup
echo -e "${YELLOW}🧹 Cleaning up...${NC}"
kill $TESTAPP_PID 2>/dev/null || true
kill $SPOOF_PID 2>/dev/null || true
sleep 1

if [ "$THRESHOLD_FAIL" = "1" ]; then
    echo -e "${RED}❌ Load test failed${NC}"
    exit 1
fi

echo -e "${GREEN}✅ Load test complete!${NC}"