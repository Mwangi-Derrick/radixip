#!/bin/bash
# Usage: ./sequential_test.sh
# Requires vegeta and jq installed

set -e

echo "🚀 Starting Sequential RadixIP Kitchen-Sink Validation Suite"

# Colors - handle CI environments without color support
if [ -t 1 ] && [ -z "${CI:-}" ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[1;33m'
    NC='\033[0m'
else
    RED=''
    GREEN=''
    YELLOW=''
    NC=''
fi

# Detect OS
detect_os() {
    case "$(uname -s)" in
        Linux*)     echo "linux";;
        Darwin*)    echo "darwin";;
        CYGWIN*|MINGW*|MSYS*) echo "windows";;
        *)          echo "unknown";;
    esac
}

OS=$(detect_os)

# Function to install jq
install_jq() {
    echo -e "${YELLOW}📦 Installing jq...${NC}"
    
    # Create bin directory
    mkdir -p "$HOME/.local/bin"
    export PATH="$HOME/.local/bin:$PATH"
    
    case "$OS" in
        linux)
            if command -v apt-get &> /dev/null; then
                sudo apt-get update && sudo apt-get install -y jq
            elif command -v yum &> /dev/null; then
                sudo yum install -y jq
            elif command -v apk &> /dev/null; then
                apk add --no-cache jq
            else
                # Download binary for Linux
                curl -L -o "$HOME/.local/bin/jq" "https://github.com/jqlang/jq/releases/latest/download/jq-linux-amd64"
                chmod +x "$HOME/.local/bin/jq"
            fi
            ;;
        darwin)
            if command -v brew &> /dev/null; then
                brew install jq
            else
                # Download binary for macOS
                curl -L -o "$HOME/.local/bin/jq" "https://github.com/jqlang/jq/releases/latest/download/jq-macos-amd64"
                chmod +x "$HOME/.local/bin/jq"
            fi
            ;;
        windows)
            # Download Windows executable
            curl -L -o "$HOME/.local/bin/jq.exe" "https://github.com/jqlang/jq/releases/latest/download/jq-win64.exe"
            chmod +x "$HOME/.local/bin/jq.exe"
            ;;
        *)
            echo -e "${RED}❌ Unsupported OS for automatic jq installation${NC}"
            echo "Please install jq manually: https://jqlang.github.io/jq/"
            exit 1
            ;;
    esac
    
    # Verify installation
    if command -v jq &> /dev/null; then
        echo -e "${GREEN}✅ jq installed successfully${NC}"
        return 0
    else
        echo -e "${RED}❌ Failed to install jq${NC}"
        return 1
    fi
}

# Check and install jq if missing
if ! command -v jq &> /dev/null; then
    echo -e "${YELLOW}⚠️  jq not found. Attempting to install...${NC}"
    if ! install_jq; then
        echo -e "${RED}❌ Please install jq manually: https://jqlang.github.io/jq/${NC}"
        exit 1
    fi
fi

# Check vegeta
if ! command -v vegeta &> /dev/null; then
    echo -e "${RED}❌ Please install vegeta: go install github.com/tsenart/vegeta@latest${NC}"
    exit 1
fi

echo -e "${YELLOW}🔨 Building Kitchen Sink Apps...${NC}"
go build -o bin/kitchen-sink-go cmd/kitchen-sink-go/main.go
cargo build --bin kitchen-sink-rust --release
echo -e "${GREEN}✅ Build complete${NC}"

# Function to kill processes on specific ports
kill_port_processes() {
    local ports=("$@")
    for port in "${ports[@]}"; do
        echo -e "${YELLOW}🔍 Checking port $port...${NC}"
        
        case "$OS" in
            windows)
                # Windows (Git Bash/MSYS2/Cygwin)
                pid=$(netstat -ano 2>/dev/null | grep ":$port " | grep LISTENING | awk '{print $NF}' | head -1)
                if [ ! -z "$pid" ]; then
                    echo -e "${YELLOW}Killing process $pid on port $port...${NC}"
                    taskkill //F //PID $pid 2>/dev/null || true
                fi
                ;;
            *)
                # Linux/macOS
                pid=$(lsof -ti :$port 2>/dev/null || true)
                if [ ! -z "$pid" ]; then
                    echo -e "${YELLOW}Killing process $pid on port $port...${NC}"
                    kill -9 $pid 2>/dev/null || true
                fi
                ;;
        esac
    done
}

echo -e "${YELLOW}🔄 Cleaning up existing processes...${NC}"

# Kill by process name (works across all platforms)
pkill -f "kitchen-sink-go" 2>/dev/null || true
pkill -f "kitchen-sink-rust" 2>/dev/null || true

# Then kill by port to be thorough
kill_port_processes 8081 8082 8083 9081 9082

sleep 3

echo -e "${YELLOW}🚀 Starting Go Kitchen Sink App...${NC}"
./bin/kitchen-sink-go &
GO_PID=$!

echo -e "${YELLOW}🚀 Starting Rust Kitchen Sink App...${NC}"
./target/release/kitchen-sink-rust &
RUST_PID=$!

sleep 5 # Wait for servers to spin up

# Function to check if service is ready
check_service() {
    local port=$1
    local max_attempts=10
    local attempt=0
    
    while [ $attempt -lt $max_attempts ]; do
        if curl -s -o /dev/null -w "%{http_code}" "http://localhost:$port/health" 2>/dev/null | grep -q "200"; then
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    return 1
}

# Check if services are ready
echo -e "${YELLOW}⏳ Waiting for services to be ready...${NC}"
for port in 8081 8082 8083 9081 9082; do
    if check_service $port; then
        echo -e "${GREEN}✅ Service on port $port is ready${NC}"
    else
        echo -e "${RED}❌ Service on port $port failed to start${NC}"
    fi
done

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
# Use unique IPs per framework to avoid auto-ban triggering and cross-framework limit sharing
IP_COUNTER=1

for target in "${TARGETS[@]}"; do
    IFS=":" read -r name port <<< "$target"
    echo -e "\n${YELLOW}Testing $name on port $port...${NC}"
    
    IP_AUTH="203.0.113.$IP_COUNTER"
    IP_PUB="203.0.114.$IP_COUNTER"
    
    cat > target_auth.txt << EOF
POST http://localhost:$port/api/v1/auth
X-Forwarded-For: $IP_AUTH
EOF
    
    cat > target_public.txt << EOF
GET http://localhost:$port/api/v1/public
X-Forwarded-For: $IP_PUB
EOF
    
    echo -e "Attacking /api/v1/auth at 1000 RPS for 2s (Expect high 429 rate)"
    vegeta attack -rate=1000 -duration=2s -targets=target_auth.txt | vegeta report -type=json > result_auth.json
    
    echo -e "Attacking /api/v1/public at 1000 RPS for 2s (Expect lower 429 rate or 200s)"
    vegeta attack -rate=1000 -duration=2s -targets=target_public.txt | vegeta report -type=json > result_public.json
    
    if [ -f result_auth.json ] && [ -f result_public.json ]; then
        auth_success=$(jq '.success' result_auth.json 2>/dev/null || echo "N/A")
        public_success=$(jq '.success' result_public.json 2>/dev/null || echo "N/A")
        echo -e "Auth Success Rate (POST, Capacity 5): $auth_success"
        echo -e "Public Success Rate (GET, Capacity 1000): $public_success"
    else
        echo -e "${RED}❌ Failed to get results for $name${NC}"
    fi
    IP_COUNTER=$((IP_COUNTER+1))
done

echo -e "\n${GREEN}==========================================${NC}"
echo -e "${GREEN} Phase 2: Auto-Ban Trigger & Sweeper Test ${NC}"
echo -e "${GREEN}==========================================${NC}"

# Attack Gin continuously to trigger auto-ban across the shared Engine
echo -e "${YELLOW}Attacking Gin (Port 8081) at 5000 RPS for 5s to trigger Auto-Ban...${NC}"
echo "GET http://localhost:8081/api/v1/public" | vegeta attack -rate=5000 -duration=5s | vegeta report -type=json > result_autoban.json

if [ -f result_autoban.json ]; then
    status_429=$(jq '.status_codes["429"] // 0' result_autoban.json 2>/dev/null || echo "0")
    status_403=$(jq '.status_codes["403"] // 0' result_autoban.json 2>/dev/null || echo "0")
    
    echo -e "429 Responses (Rate Limited): $status_429"
    echo -e "403 Responses (Auto-Banned): $status_403"
    
    if [ "$status_403" -gt 0 ]; then
        echo -e "${GREEN}✅ Auto-Ban triggered successfully!${NC}"
    else
        echo -e "${RED}❌ Auto-Ban failed to trigger.${NC}"
    fi
else
    echo -e "${RED}❌ Failed to get auto-ban results${NC}"
fi

echo -e "\n${YELLOW}Waiting 35 seconds for Sweeper to remove the ban...${NC}"
sleep 35

echo -e "Sending single request to Gin (Port 8081) to verify ban lifted..."
STATUS=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8081/api/v1/public 2>/dev/null || echo "000")
if [ "$STATUS" -eq 200 ]; then
    echo -e "${GREEN}✅ Sweeper successfully lifted the ban (Status 200 OK)!${NC}"
elif [ "$STATUS" -eq 000 ]; then
    echo -e "${RED}❌ Could not connect to server${NC}"
else
    echo -e "${RED}❌ IP is still blocked or limited (Status $STATUS)!${NC}"
fi

echo -e "\n${YELLOW}🧹 Cleaning up...${NC}"

# Kill specific PIDs (works on all platforms)
if [ ! -z "$GO_PID" ]; then
    kill $GO_PID 2>/dev/null || true
fi
if [ ! -z "$RUST_PID" ]; then
    kill $RUST_PID 2>/dev/null || true
fi

# Kill any remaining processes by port
kill_port_processes 8081 8082 8083 9081 9082

# Clean up temporary files
rm -f target_auth.txt target_public.txt result_auth.json result_public.json result_autoban.json

echo -e "${GREEN}✅ All Tests Completed!${NC}"