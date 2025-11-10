#!/bin/bash

# Comprehensive End-to-End Test Script
# Tests:
#   1. Page Server with all storage backends (File, S3, Hybrid)
#   2. Full serverless stack integration (Control Plane + Page Server + Safekeeper + MariaDB)

set -e

# Configuration
PAGE_SERVER_PORT=${PAGE_SERVER_PORT:-8090}
TEST_DATA_DIR=${TEST_DATA_DIR:-/tmp/test-page-server-e2e}
TEST_API_KEY=${TEST_API_KEY:-test-e2e-key-$(date +%s)}
PAGE_SERVER_PID=""
PAGE_SERVER_LOG="/tmp/page-server-e2e.log"

# Wasabi S3 Configuration (for S3 and Hybrid tests)
S3_ENDPOINT="https://s3.wasabisys.com"
S3_BUCKET="sb-mariadb"
S3_REGION="us-east-1"
S3_ACCESS_KEY="X7SMWFBIMHK761MZDCM4"
S3_SECRET_KEY="HeCjI9zsWe6lemh42fmCCugfyF06f7zXlyb9VY0G"

# Full Integration Test Configuration
CONTROL_PLANE_URL="${CONTROL_PLANE_URL:-http://localhost:8080}"
HOST_IP="${HOST_IP:-$(hostname -I 2>/dev/null | awk '{print $1}' || ip route get 8.8.8.8 2>/dev/null | awk '{print $7; exit}' || echo 'host.docker.internal')}"
PAGE_SERVER_URL_INTEGRATION="${PAGE_SERVER_URL_INTEGRATION:-http://${HOST_IP}:8081}"
SAFEKEEPER_URL="${SAFEKEEPER_URL:-http://${HOST_IP}:8082}"
MARIADB_IMAGE="${MARIADB_IMAGE:-stackblaze/mariadb-pageserver:latest}"

# Directories
PAGE_SERVER_DIR="${PAGE_SERVER_DIR:-./page-server}"
SAFEKEEPER_DIR="${SAFEKEEPER_DIR:-./safekeeper}"
CONTROL_PLANE_DIR="${CONTROL_PLANE_DIR:-./control-plane}"

# Integration test PIDs
PAGE_SERVER_PID_INTEGRATION=""
SAFEKEEPER_PID=""
CONTROL_PLANE_PID=""

# Test mode flags (can be overridden)
RUN_STORAGE_TESTS=${RUN_STORAGE_TESTS:-true}
RUN_INTEGRATION_TEST=${RUN_INTEGRATION_TEST:-true}

# Kill any existing Page Server on this port
lsof -ti:${PAGE_SERVER_PORT} | xargs kill -9 2>/dev/null || true
sleep 1

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test counters
TESTS_PASSED=0
TESTS_FAILED=0

# Helper functions
log_info() {
    echo -e "${GREEN}✓${NC} $1"
}

log_error() {
    echo -e "${RED}✗${NC} $1"
    TESTS_FAILED=$((TESTS_FAILED + 1))
}

log_warn() {
    echo -e "${YELLOW}⚠${NC} $1"
}

log_test() {
    echo -e "${BLUE}[TEST]${NC} $1"
}

test_pass() {
    TESTS_PASSED=$((TESTS_PASSED + 1))
    log_info "$1"
}

test_fail() {
    log_error "$1"
}

# Cleanup function for storage tests
cleanup() {
    if [ -n "$PAGE_SERVER_PID" ]; then
        echo ""
        echo "Stopping Page Server (PID: $PAGE_SERVER_PID)..."
        kill $PAGE_SERVER_PID 2>/dev/null || true
        wait $PAGE_SERVER_PID 2>/dev/null || true
    fi
}

# Cleanup function for integration test
cleanup_integration() {
    echo ""
    echo -e "${YELLOW}Cleaning up integration test services...${NC}"
    
    if [ -n "$PAGE_SERVER_PID_INTEGRATION" ]; then
        kill $PAGE_SERVER_PID_INTEGRATION 2>/dev/null || true
        echo "   Stopped Page Server (integration)"
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
    
    echo -e "${GREEN}Integration cleanup complete${NC}"
}

trap cleanup EXIT

# Start Page Server with specified backend
start_page_server() {
    local backend=$1
    echo "=== Starting Page Server (Backend: $backend) ==="
    
    # Check if Page Server binary exists
    if [ ! -f "./page-server/page-server" ]; then
        log_error "Page Server binary not found. Building..."
        cd page-server
        go build -o page-server *.go
        cd ..
    fi
    
    # Clean up old test data
    rm -rf "$TEST_DATA_DIR"
    
    # Build command based on backend
    local cmd="./page-server/page-server -port $PAGE_SERVER_PORT -data-dir \"$TEST_DATA_DIR\" -cache-size 100 -api-key \"$TEST_API_KEY\""
    
    case "$backend" in
        "file")
            # File storage (default)
            ;;
        "s3")
            cmd="$cmd -storage-backend s3 -s3-endpoint $S3_ENDPOINT -s3-bucket $S3_BUCKET -s3-region $S3_REGION -s3-access-key $S3_ACCESS_KEY -s3-secret-key $S3_SECRET_KEY -s3-use-ssl true"
            ;;
        "hybrid")
            cmd="$cmd -storage-backend hybrid -s3-endpoint $S3_ENDPOINT -s3-bucket $S3_BUCKET -s3-region $S3_REGION -s3-access-key $S3_ACCESS_KEY -s3-secret-key $S3_SECRET_KEY -s3-use-ssl true"
            ;;
    esac
    
    # Start Page Server
    eval "$cmd > \"$PAGE_SERVER_LOG\" 2>&1 &"
    PAGE_SERVER_PID=$!
    echo "Page Server started (PID: $PAGE_SERVER_PID, Backend: $backend)"
    
    # Wait for server to start
    echo "Waiting for Page Server to start..."
    for i in {1..10}; do
        if curl -s http://localhost:${PAGE_SERVER_PORT}/api/v1/ping > /dev/null 2>&1; then
            log_info "Page Server is ready"
            return 0
        fi
        sleep 1
    done
    
    log_error "Page Server failed to start"
    cat "$PAGE_SERVER_LOG"
    exit 1
}

# Common test functions
test_ping() {
    RESPONSE=$(curl -s http://localhost:${PAGE_SERVER_PORT}/api/v1/ping)
    if echo "$RESPONSE" | grep -q '"status":"ok"'; then
        test_pass "Ping endpoint working"
    else
        test_fail "Ping endpoint failed: $RESPONSE"
    fi
}

test_authentication() {
    HTTP_CODE=$(curl -s -o /tmp/auth_test_response.txt -w "%{http_code}" -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/get_page \
        -H "Content-Type: application/json" \
        -d '{"space_id":1,"page_no":42,"lsn":1000}')
    BODY=$(cat /tmp/auth_test_response.txt)
    
    if [ "$HTTP_CODE" = "401" ] && echo "$BODY" | grep -q "Authentication required"; then
        test_pass "Authentication required (correctly blocks unauthorized)"
    else
        test_fail "Authentication not working: HTTP $HTTP_CODE"
    fi
}

test_wal_and_page() {
    # Stream WAL
    WAL_DATA=$(echo -n "Test WAL Data" | base64)
    RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/stream_wal \
        -H "Content-Type: application/json" \
        -d "{\"lsn\":1000,\"wal_data\":\"$WAL_DATA\",\"space_id\":1,\"page_no\":42}")
    
    if echo "$RESPONSE" | grep -q '"status":"success"'; then
        test_pass "WAL streaming successful"
    else
        test_fail "WAL streaming failed: $RESPONSE"
        return
    fi
    
    sleep 1
    
    # Get page
    RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/get_page \
        -H "Content-Type: application/json" \
        -d '{"space_id":1,"page_no":42,"lsn":1000}')
    
    if echo "$RESPONSE" | grep -q '"status":"success"'; then
        test_pass "Page retrieval successful"
    else
        test_fail "Page retrieval failed: $RESPONSE"
    fi
}

test_batch_operations() {
    # Create pages
    WAL_DATA1=$(echo -n "Batch Test 1" | base64)
    curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/stream_wal \
        -H "Content-Type: application/json" \
        -d "{\"lsn\":5000,\"wal_data\":\"$WAL_DATA1\",\"space_id\":1,\"page_no\":200}" > /dev/null
    
    WAL_DATA2=$(echo -n "Batch Test 2" | base64)
    curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/stream_wal \
        -H "Content-Type: application/json" \
        -d "{\"lsn\":6000,\"wal_data\":\"$WAL_DATA2\",\"space_id\":1,\"page_no\":201}" > /dev/null
    
    sleep 1
    
    # Test batch
    RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/get_pages \
        -H "Content-Type: application/json" \
        -d '{"pages":[{"space_id":1,"page_no":200,"lsn":5000},{"space_id":1,"page_no":201,"lsn":6000}]}')
    
    SUCCESS_COUNT=$(echo "$RESPONSE" | python3 -c "import sys, json; d=json.load(sys.stdin); print(sum(1 for p in d.get('pages', []) if p.get('status') == 'success'))" 2>/dev/null || echo "0")
    if [ "$SUCCESS_COUNT" -eq 2 ]; then
        test_pass "Batch operations successful ($SUCCESS_COUNT/2)"
    else
        test_fail "Batch operations failed: $SUCCESS_COUNT/2"
    fi
}

test_metrics() {
    RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
        http://localhost:${PAGE_SERVER_PORT}/api/v1/metrics)
    
    if echo "$RESPONSE" | grep -q '"cache"'; then
        test_pass "Metrics endpoint working"
    else
        test_fail "Metrics endpoint failed: $RESPONSE"
    fi
}

test_time_travel() {
    WAL_DATA1=$(echo -n "Time Travel 1" | base64)
    curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/stream_wal \
        -H "Content-Type: application/json" \
        -d "{\"lsn\":7000,\"wal_data\":\"$WAL_DATA1\",\"space_id\":1,\"page_no\":300}" > /dev/null
    
    sleep 1
    
    RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/time_travel \
        -H "Content-Type: application/json" \
        -d '{"space_id":1,"page_no":300,"lsn":7000}')
    
    if echo "$RESPONSE" | grep -q '"status":"success"'; then
        test_pass "Time-travel query successful"
    else
        test_fail "Time-travel query failed: $RESPONSE"
    fi
}

test_snapshots() {
    RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/snapshots/create \
        -H "Content-Type: application/json" \
        -d '{"description":"E2E test snapshot"}')
    
    if echo "$RESPONSE" | grep -q '"status":"success"'; then
        test_pass "Snapshot creation successful"
    else
        test_fail "Snapshot creation failed: $RESPONSE"
    fi
}

# Test suite for file storage
test_file_storage() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     File Storage Backend Tests                              ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    
    start_page_server "file"
    
    test_ping
    test_authentication
    test_wal_and_page
    test_batch_operations
    test_metrics
    test_time_travel
    test_snapshots
    
    cleanup
    sleep 2
}

# Test suite for S3 storage
test_s3_storage() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     S3 Storage Backend Tests                               ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    
    start_page_server "s3"
    
    test_ping
    test_authentication
    test_wal_and_page
    test_batch_operations
    test_metrics
    test_time_travel
    test_snapshots
    
    cleanup
    sleep 2
}

# Test suite for hybrid storage
test_hybrid_storage() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     Hybrid Storage (Neon-Style Tiered Caching) Tests        ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    
    start_page_server "hybrid"
    
    test_ping
    test_authentication
    test_wal_and_page
    test_batch_operations
    test_metrics
    test_time_travel
    test_snapshots
    
    # Test tiered storage metrics
    RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
        http://localhost:${PAGE_SERVER_PORT}/api/v1/metrics)
    
    if echo "$RESPONSE" | grep -q '"tiered_storage"'; then
        test_pass "Tiered storage metrics available"
    else
        test_fail "Tiered storage metrics not available"
    fi
    
    cleanup
    sleep 2
}

# ============================================================================
# Full Integration Test Functions (from test_full_integration.sh)
# ============================================================================

wait_for_service() {
    local url=$1
    local name=$2
    local max_attempts=30
    local attempt=0
    
    log_info "Waiting for $name to be ready..."
    while [ $attempt -lt $max_attempts ]; do
        if curl -s -f "$url" > /dev/null 2>&1; then
            test_pass "$name is ready"
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    
    test_fail "$name is not responding after $max_attempts attempts"
    return 1
}

start_page_server_integration() {
    log_test "Starting Page Server for integration test..."
    
    if [ ! -f "$PAGE_SERVER_DIR/page-server" ]; then
        log_info "Page Server binary not found, building..."
        if [ ! -f "$PAGE_SERVER_DIR/build.sh" ]; then
            test_fail "build.sh not found in $PAGE_SERVER_DIR"
            return 1
        fi
        cd "$PAGE_SERVER_DIR"
        chmod +x build.sh
        ./build.sh
        if [ ! -f "./page-server" ]; then
            test_fail "Failed to build Page Server"
            cd ..
            return 1
        fi
        cd ..
    fi
    
    cd "$PAGE_SERVER_DIR"
    mkdir -p ./page-server-data
    ./page-server -port 8081 -data-dir ./page-server-data > page-server.log 2>&1 &
    PAGE_SERVER_PID_INTEGRATION=$!
    cd ..
    
    sleep 2
    wait_for_service "$PAGE_SERVER_URL_INTEGRATION/api/v1/ping" "Page Server"
}

start_safekeeper_integration() {
    log_test "Starting Safekeeper..."
    
    if [ ! -f "$SAFEKEEPER_DIR/safekeeper" ]; then
        log_info "Safekeeper binary not found, building..."
        if [ ! -f "$SAFEKEEPER_DIR/build.sh" ]; then
            test_fail "build.sh not found in $SAFEKEEPER_DIR"
            return 1
        fi
        cd "$SAFEKEEPER_DIR"
        chmod +x build.sh
        ./build.sh
        if [ ! -f "./safekeeper" ]; then
            test_fail "Failed to build Safekeeper"
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
    
    sleep 2
    wait_for_service "$SAFEKEEPER_URL/api/v1/ping" "Safekeeper"
}

start_control_plane_integration() {
    log_test "Starting Control Plane..."
    
    if [ ! -f "$CONTROL_PLANE_DIR/control-plane" ]; then
        log_info "Control Plane binary not found, building..."
        cd "$CONTROL_PLANE_DIR" && go build -o control-plane ./cmd/api && cd ..
    fi
    
    export MARIADB_PAGESERVER_IMAGE="$MARIADB_IMAGE"
    cd "$CONTROL_PLANE_DIR"
    ./control-plane -port 8080 -db-type sqlite > control-plane.log 2>&1 &
    CONTROL_PLANE_PID=$!
    cd ..
    
    wait_for_service "$CONTROL_PLANE_URL/api/v1/projects" "Control Plane"
}

test_full_integration() {
    log_test "Testing full serverless stack integration..."
    
    # Create project
    log_info "Creating project with Page Server: $PAGE_SERVER_URL_INTEGRATION and Safekeeper: $SAFEKEEPER_URL"
    PROJECT_RESPONSE=$(curl -s -X POST "$CONTROL_PLANE_URL/api/v1/projects" \
        -H "Content-Type: application/json" \
        -d "{
            \"name\": \"integration-test\",
            \"config\": {
                \"page_server_url\": \"$PAGE_SERVER_URL_INTEGRATION\",
                \"safekeeper_url\": \"$SAFEKEEPER_URL\",
                \"idle_timeout\": 300,
                \"max_connections\": 100
            }
        }")
    
    PROJECT_ID=$(echo "$PROJECT_RESPONSE" | jq -r '.id' 2>/dev/null || echo "")
    if [ -z "$PROJECT_ID" ] || [ "$PROJECT_ID" = "null" ]; then
        test_fail "Failed to create project: $PROJECT_RESPONSE"
        return 1
    fi
    
    test_pass "Project created: $PROJECT_ID"
    
    # Create compute node
    log_info "Creating compute node with image: $MARIADB_IMAGE"
    COMPUTE_RESPONSE=$(curl -s -X POST "$CONTROL_PLANE_URL/api/v1/projects/$PROJECT_ID/compute" \
        -H "Content-Type: application/json" \
        -d "{
            \"config\": {
                \"image\": \"$MARIADB_IMAGE\",
                \"page_server_url\": \"$PAGE_SERVER_URL_INTEGRATION\",
                \"safekeeper_url\": \"$SAFEKEEPER_URL\",
                \"resources\": {
                    \"cpu\": \"100m\",
                    \"memory\": \"256Mi\"
                }
            }
        }")
    
    COMPUTE_ID=$(echo "$COMPUTE_RESPONSE" | jq -r '.id' 2>/dev/null || echo "")
    if [ -z "$COMPUTE_ID" ] || [ "$COMPUTE_ID" = "null" ]; then
        test_fail "Failed to create compute node: $COMPUTE_RESPONSE"
        return 1
    fi
    
    test_pass "Compute node created: $COMPUTE_ID"
    
    # Wait for pod to be ready
    log_info "Waiting for MariaDB pod to be ready (this may take a few minutes)..."
    POD_NAME=$(kubectl get pods -l compute-id=$COMPUTE_ID -o jsonpath='{.items[0].metadata.name}' 2>/dev/null || echo "")
    
    if [ -z "$POD_NAME" ]; then
        test_fail "Pod not found for compute node $COMPUTE_ID"
        return 1
    fi
    
    # Wait up to 5 minutes for pod to be ready
    POD_STATUS=""
    for i in {1..60}; do
        POD_STATUS=$(kubectl get pod $POD_NAME -o jsonpath='{.status.phase}' 2>/dev/null || echo "Unknown")
        if [ "$POD_STATUS" = "Running" ]; then
            test_pass "Pod is running: $POD_NAME"
            break
        fi
        if [ "$POD_STATUS" = "Failed" ] || [ "$POD_STATUS" = "Error" ]; then
            test_fail "Pod failed: $POD_NAME"
            kubectl describe pod $POD_NAME 2>/dev/null || true
            return 1
        fi
        sleep 5
    done
    
    if [ "$POD_STATUS" != "Running" ]; then
        test_fail "Pod did not become ready in time"
        kubectl describe pod $POD_NAME 2>/dev/null || true
        return 1
    fi
    
    # Verify connections
    log_info "Checking Page Server metrics..."
    curl -s "$PAGE_SERVER_URL_INTEGRATION/api/v1/metrics" | grep -i "page\|request" | head -5 || log_warn "Metrics endpoint not available"
    
    log_info "Checking Safekeeper metrics..."
    curl -s "$SAFEKEEPER_URL/api/v1/metrics" | grep -i "wal\|request" | head -5 || log_warn "Metrics endpoint not available"
    
    test_pass "Full integration test completed!"
    
    # Cleanup test resources
    log_info "Cleaning up test resources..."
    curl -s -X DELETE "$CONTROL_PLANE_URL/api/v1/projects/$PROJECT_ID" > /dev/null
    test_pass "Test resources cleaned up"
}

# ============================================================================
# Main test execution
# ============================================================================

main() {
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     Comprehensive End-to-End Test Suite                      ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    echo ""
    
    # Reset counters
    TESTS_PASSED=0
    TESTS_FAILED=0
    
    # Part 1: Storage Backend Tests
    if [ "$RUN_STORAGE_TESTS" = "true" ]; then
        echo "╔══════════════════════════════════════════════════════════════╗"
        echo "║  PART 1: Page Server Storage Backend Tests                 ║"
        echo "╚══════════════════════════════════════════════════════════════╝"
        echo ""
        echo "Testing all storage backends:"
        echo "  1. File Storage"
        echo "  2. S3 Storage (Wasabi)"
        echo "  3. Hybrid Storage (Neon-style tiered caching)"
        echo ""
        
        test_file_storage
        test_s3_storage
        test_hybrid_storage
    else
        log_warn "Skipping storage backend tests (RUN_STORAGE_TESTS=false)"
    fi
    
    # Part 2: Full Integration Test
    if [ "$RUN_INTEGRATION_TEST" = "true" ]; then
        echo ""
        echo "╔══════════════════════════════════════════════════════════════╗"
        echo "║  PART 2: Full Serverless Stack Integration Test             ║"
        echo "╚══════════════════════════════════════════════════════════════╝"
        echo ""
        echo "Testing complete serverless stack:"
        echo "  - Control Plane"
        echo "  - Page Server"
        echo "  - Safekeeper"
        echo "  - MariaDB Compute Node (Kubernetes)"
        echo ""
        
        # Check prerequisites
        if ! command -v kubectl > /dev/null 2>&1; then
            test_fail "kubectl not found. Please install kubectl."
            RUN_INTEGRATION_TEST=false
        elif ! kubectl cluster-info > /dev/null 2>&1; then
            test_fail "Kubernetes cluster not accessible. Please check kubeconfig."
            RUN_INTEGRATION_TEST=false
        else
            # Setup cleanup trap for integration test
            trap cleanup_integration EXIT
            
            # Start services
            start_page_server_integration
            start_safekeeper_integration
            start_control_plane_integration
            
            # Run integration test
            test_full_integration
            
            # Cleanup
            cleanup_integration
            trap cleanup EXIT
        fi
    else
        log_warn "Skipping integration test (RUN_INTEGRATION_TEST=false)"
    fi
    
    # Print final summary
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║                    Final Test Summary                        ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    echo ""
    echo "Tests Passed: $TESTS_PASSED"
    echo "Tests Failed: $TESTS_FAILED"
    echo ""
    
    if [ $TESTS_FAILED -eq 0 ]; then
        echo -e "${GREEN}✓ All tests passed!${NC}"
        exit 0
    else
        echo -e "${RED}✗ Some tests failed${NC}"
        exit 1
    fi
}

# Run main function
main

