#!/bin/bash
set -e  # Exit on error

echo "🚀 Starting local load test..."

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

# Seed blocklist
echo -e "${YELLOW}🌱 Seeding blocklist with 100k CIDRs...${NC}"
./bin/seed_blocklist -count 100000 -addr localhost:8082
echo -e "${GREEN}✅ Blocklist seeded${NC}"

# Run load test
echo -e "${YELLOW}📊 Running load test (50k req/s for 30s)...${NC}"
echo "GET http://localhost:8080/ping" | \
    vegeta attack -rate=50000 -duration=30s -timeout=5s | \
    vegeta report -type=text | tee load_test_results.txt

# Parse results
echo ""
echo -e "${GREEN}=== Load Test Results ===${NC}"
cat load_test_results.txt

# Check p99
P99=$(grep "99th" load_test_results.txt | grep -oP '[\d.]+(ms|s)' | head -1)
echo ""
echo -e "📊 p99 latency: ${YELLOW}$P99${NC}"

# Convert to milliseconds for comparison
if [[ $P99 == *"s"* ]]; then
    VALUE=$(echo $P99 | sed 's/s//' | awk '{print $1 * 1000}')
elif [[ $P99 == *"ms"* ]]; then
    VALUE=$(echo $P99 | sed 's/ms//')
else
    echo -e "${RED}⚠️  Could not parse p99 value: $P99${NC}"
fi

# Check threshold (100ms)
if (( $(echo "$VALUE > 100" | bc -l 2>/dev/null || echo "0") )); then
    echo -e "${RED}❌ p99 latency ($VALUE ms) exceeds 100ms threshold${NC}"
    THRESHOLD_FAIL=1
else
    echo -e "${GREEN}✅ p99 latency ($VALUE ms) within acceptable range${NC}"
fi

# Cleanup
echo -e "${YELLOW}🧹 Cleaning up...${NC}"
kill $TESTAPP_PID 2>/dev/null
kill $SPOOF_PID 2>/dev/null

if [ "$THRESHOLD_FAIL" = "1" ]; then
    exit 1
fi

echo -e "${GREEN}✅ Load test complete!${NC}"