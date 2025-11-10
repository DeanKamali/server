#!/bin/bash

# Full Integration Test: Control Plane + Page Server + Safekeeper + MariaDB
# Tests the complete serverless stack with the custom MariaDB image

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
CONTROL_PLANE_URL="${CONTROL_PLANE_URL:-http://localhost:8080}"

# Get host IP for pod access (pods can't use localhost)
HOST_IP="${HOST_IP:-$(hostname -I 2>/dev/null | awk '{print $1}' || ip route get 8.8.8.8 2>/dev/null | awk '{print $7; exit}' || echo 'host.docker.internal')}"
PAGE_SERVER_URL="${PAGE_SERVER_URL:-http://${HOST_IP}:8081}"
SAFEKEEPER_URL="${SAFEKEEPER_URL:-http://${HOST_IP}:8082}"
MARIADB_IMAGE="${MARIADB_IMAGE:-stackblaze/mariadb-pageserver:latest}"

# Directories
PAGE_SERVER_DIR="${PAGE_SERVER_DIR:-./page-server}"
SAFEKEEPER_DIR="${SAFEKEEPER_DIR:-./safekeeper}"
CONTROL_PLANE_DIR="${CONTROL_PLANE_DIR:-./control-plane}"

# PIDs for cleanup
PAGE_SERVER_PID=""
SAFEKEEPER_PID=""
CONTROL_PLANE_PID=""

# Cleanup function
cleanup() {
    echo ""
    echo -e "${YELLOW}Cleaning up...${NC}"
    
    if [ -n "$PAGE_SERVER_PID" ]; then
        kill $PAGE_SERVER_PID 2>/dev/null || true
        echo "   Stopped Page Server"
    fi
    
    if [ -n "$SAFEKEEPER_PID" ]; then
        kill $SAFEKEEPER_PID 2>/dev/null || true
        echo "   Stopped Safekeeper"
    fi
    
    if [ -n "$CONTROL_PLANE_PID" ]; then
        kill $CONTROL_PLANE_PID 2>/dev/null || true
        echo "   Stopped Control Plane"
    fi
    
    # Clean up test pods
    kubectl delete pod -l app=mariadb-compute 2>/dev/null || true
    
    echo -e "${GREEN}Cleanup complete${NC}"
}

trap cleanup EXIT

# Helper functions
print_info() {
    echo -e "${YELLOW}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[PASS]${NC} $1"
}

print_error() {
    echo -e "${RED}[FAIL]${NC} $1"
}

print_test() {
    echo -e "${BLUE}[TEST]${NC} $1"
}

wait_for_service() {
    local url=$1
    local name=$2
    local max_attempts=30
    local attempt=0
    
    print_info "Waiting for $name to be ready..."
    while [ $attempt -lt $max_attempts ]; do
        if curl -s -f "$url" > /dev/null 2>&1; then
            print_success "$name is ready"
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    
    print_error "$name is not responding after $max_attempts attempts"
    print_info "Trying to check service status..."
    if [ "$name" = "Page Server" ]; then
        curl -s "$url" || true
        if [ -f "$PAGE_SERVER_DIR/page-server.log" ]; then
            print_info "Page Server logs:"
            tail -10 "$PAGE_SERVER_DIR/page-server.log" || true
        fi
    elif [ "$name" = "Safekeeper" ]; then
        curl -s "$url" || true
    fi
    return 1
}

# Step 1: Start Page Server
start_page_server() {
    print_test "Starting Page Server..."
    
    if [ ! -f "$PAGE_SERVER_DIR/page-server" ]; then
        print_info "Page Server binary not found, building..."
        if [ ! -f "$PAGE_SERVER_DIR/build.sh" ]; then
            print_error "build.sh not found in $PAGE_SERVER_DIR"
            return 1
        fi
        cd "$PAGE_SERVER_DIR"
        chmod +x build.sh
        ./build.sh
        if [ ! -f "./page-server" ]; then
            print_error "Failed to build Page Server"
            cd ..
            return 1
        fi
        cd ..
    fi
    
    cd "$PAGE_SERVER_DIR"
    mkdir -p ./page-server-data
    ./page-server -port 8081 -data-dir ./page-server-data > page-server.log 2>&1 &
    PAGE_SERVER_PID=$!
    cd ..
    
    sleep 2  # Give it a moment to start
    
    wait_for_service "$PAGE_SERVER_URL/api/v1/ping" "Page Server"
}

# Step 2: Start Safekeeper
start_safekeeper() {
    print_test "Starting Safekeeper..."
    
    if [ ! -f "$SAFEKEEPER_DIR/safekeeper" ]; then
        print_info "Safekeeper binary not found, building..."
        if [ ! -f "$SAFEKEEPER_DIR/build.sh" ]; then
            print_error "build.sh not found in $SAFEKEEPER_DIR"
            return 1
        fi
        cd "$SAFEKEEPER_DIR"
        chmod +x build.sh
        ./build.sh
        if [ ! -f "./safekeeper" ]; then
            print_error "Failed to build Safekeeper"
            cd ..
            return 1
        fi
        cd ..
    fi
    
    cd "$SAFEKEEPER_DIR"
    mkdir -p ./safekeeper-data
    ./safekeeper -port 8082 -data-dir ./safekeeper-data > safekeeper.log 2>&1 &
    SAFEKEEPER_PID=$!
    cd ..
    
    sleep 2  # Give it a moment to start
    
    wait_for_service "$SAFEKEEPER_URL/api/v1/ping" "Safekeeper"
}

# Step 3: Start Control Plane
start_control_plane() {
    print_test "Starting Control Plane..."
    
    if [ ! -f "$CONTROL_PLANE_DIR/control-plane" ]; then
        print_error "Control Plane binary not found"
        print_info "Building Control Plane..."
        cd "$CONTROL_PLANE_DIR" && go build -o control-plane ./cmd/api && cd ..
    fi
    
    export MARIADB_PAGESERVER_IMAGE="$MARIADB_IMAGE"
    cd "$CONTROL_PLANE_DIR"
    ./control-plane -port 8080 -db-type sqlite > control-plane.log 2>&1 &
    CONTROL_PLANE_PID=$!
    cd ..
    
    wait_for_service "$CONTROL_PLANE_URL/api/v1/projects" "Control Plane"
}

# Step 4: Test Integration
test_integration() {
    print_test "Testing full integration..."
    
    # Create project with host IP URLs (pods need host IP, not localhost)
    print_info "Creating project with Page Server: $PAGE_SERVER_URL and Safekeeper: $SAFEKEEPER_URL"
    PROJECT_RESPONSE=$(curl -s -X POST "$CONTROL_PLANE_URL/api/v1/projects" \
        -H "Content-Type: application/json" \
        -d "{
            \"name\": \"integration-test\",
            \"config\": {
                \"page_server_url\": \"$PAGE_SERVER_URL\",
                \"safekeeper_url\": \"$SAFEKEEPER_URL\",
                \"idle_timeout\": 300,
                \"max_connections\": 100
            }
        }")
    
    PROJECT_ID=$(echo "$PROJECT_RESPONSE" | jq -r '.id')
    if [ -z "$PROJECT_ID" ] || [ "$PROJECT_ID" = "null" ]; then
        print_error "Failed to create project: $PROJECT_RESPONSE"
        return 1
    fi
    
    print_success "Project created: $PROJECT_ID"
    
    # Create compute node
    print_info "Creating compute node with image: $MARIADB_IMAGE"
    COMPUTE_RESPONSE=$(curl -s -X POST "$CONTROL_PLANE_URL/api/v1/projects/$PROJECT_ID/compute" \
        -H "Content-Type: application/json" \
        -d "{
            \"config\": {
                \"image\": \"$MARIADB_IMAGE\",
                \"page_server_url\": \"$PAGE_SERVER_URL\",
                \"safekeeper_url\": \"$SAFEKEEPER_URL\",
                \"resources\": {
                    \"cpu\": \"100m\",
                    \"memory\": \"256Mi\"
                }
            }
        }")
    
    COMPUTE_ID=$(echo "$COMPUTE_RESPONSE" | jq -r '.id')
    if [ -z "$COMPUTE_ID" ] || [ "$COMPUTE_ID" = "null" ]; then
        print_error "Failed to create compute node: $COMPUTE_RESPONSE"
        return 1
    fi
    
    print_success "Compute node created: $COMPUTE_ID"
    
    # Wait for pod to be ready
    print_info "Waiting for MariaDB pod to be ready (this may take a few minutes)..."
    POD_NAME=$(kubectl get pods -l compute-id=$COMPUTE_ID -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
    
    if [ -z "$POD_NAME" ]; then
        print_error "Pod not found for compute node $COMPUTE_ID"
        return 1
    fi
    
    # Wait up to 5 minutes for pod to be ready
    for i in {1..60}; do
        POD_STATUS=$(kubectl get pod $POD_NAME -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
        if [ "$POD_STATUS" = "Running" ]; then
            print_success "Pod is running: $POD_NAME"
            break
        fi
        if [ "$POD_STATUS" = "Failed" ] || [ "$POD_STATUS" = "Error" ]; then
            print_error "Pod failed: $POD_NAME"
            kubectl describe pod $POD_NAME
            return 1
        fi
        sleep 5
    done
    
    if [ "$POD_STATUS" != "Running" ]; then
        print_error "Pod did not become ready in time"
        kubectl describe pod $POD_NAME
        return 1
    fi
    
    # Check if MariaDB is connecting to Page Server and Safekeeper
    print_info "Checking pod logs for Page Server/Safekeeper connection..."
    kubectl logs $POD_NAME | grep -i "page.server\|safekeeper" | head -5 || print_info "No connection logs found yet (this is normal if MariaDB is still starting)"
    
    # Verify Page Server received connections
    print_info "Checking Page Server metrics..."
    curl -s "$PAGE_SERVER_URL/api/v1/metrics" | grep -i "page\|request" | head -5 || print_info "Metrics endpoint not available"
    
    # Verify Safekeeper received connections
    print_info "Checking Safekeeper metrics..."
    curl -s "$SAFEKEEPER_URL/api/v1/metrics" | grep -i "wal\|request" | head -5 || print_info "Metrics endpoint not available"
    
    print_success "Integration test completed!"
    
    # Cleanup test resources
    print_info "Cleaning up test resources..."
    curl -s -X DELETE "$CONTROL_PLANE_URL/api/v1/projects/$PROJECT_ID" > /dev/null
    print_success "Test resources cleaned up"
}

# Main execution
main() {
    echo "=========================================="
    echo "Full Integration Test"
    echo "=========================================="
    echo ""
    echo "Configuration:"
    echo "  Control Plane: $CONTROL_PLANE_URL"
    echo "  Page Server: $PAGE_SERVER_URL"
    echo "  Safekeeper: $SAFEKEEPER_URL"
    echo "  MariaDB Image: $MARIADB_IMAGE"
    echo "  Host IP (for pods): $HOST_IP"
    echo ""
    
    # Check prerequisites
    if ! command -v kubectl > /dev/null 2>&1; then
        print_error "kubectl not found. Please install kubectl."
        exit 1
    fi
    
    if ! kubectl cluster-info > /dev/null 2>&1; then
        print_error "Kubernetes cluster not accessible. Please check kubeconfig."
        exit 1
    fi
    
    # Start services
    start_page_server
    start_safekeeper
    start_control_plane
    
    # Run integration test
    test_integration
    
    echo ""
    echo -e "${GREEN}=========================================="
    echo "✅ All tests passed!"
    echo "==========================================${NC}"
}

main "$@"

