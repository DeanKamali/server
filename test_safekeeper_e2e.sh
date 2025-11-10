#!/bin/bash

# Comprehensive End-to-End Test Script for Safekeeper + Page Server
# Tests the full flow: MariaDB → Safekeeper → Page Server → Database

set -e

# Configuration
SAFEKEEPER_PORT=${SAFEKEEPER_PORT:-8090}
PAGE_SERVER_PORT=${PAGE_SERVER_PORT:-8080}
TEST_DATA_DIR=${TEST_DATA_DIR:-/tmp/test-safekeeper-e2e}
TEST_API_KEY=${TEST_API_KEY:-test-safekeeper-key-$(date +%s)}
SAFEKEEPER_PID=""
PAGE_SERVER_PID=""
SAFEKEEPER_LOG="/tmp/safekeeper-e2e.log"
PAGE_SERVER_LOG="/tmp/page-server-e2e.log"

# Kill any existing processes
lsof -ti:${SAFEKEEPER_PORT} | xargs kill -9 2>/dev/null || true
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

log_step() {
    echo -e "${BLUE}→${NC} $1"
}

test_pass() {
    TESTS_PASSED=$((TESTS_PASSED + 1))
    log_info "$1"
}

test_fail() {
    log_error "$1"
}

# Cleanup function
cleanup() {
    echo ""
    log_step "Cleaning up..."
    
    if [ -n "$PAGE_SERVER_PID" ]; then
        log_step "Stopping Page Server (PID: $PAGE_SERVER_PID)..."
        kill $PAGE_SERVER_PID 2>/dev/null || true
        wait $PAGE_SERVER_PID 2>/dev/null || true
    fi
    
    if [ -n "$SAFEKEEPER_PID" ]; then
        log_step "Stopping Safekeeper (PID: $SAFEKEEPER_PID)..."
        kill $SAFEKEEPER_PID 2>/dev/null || true
        wait $SAFEKEEPER_PID 2>/dev/null || true
    fi
}

trap cleanup EXIT

# Start Safekeeper
start_safekeeper() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     Starting Safekeeper                                      ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    
    # Check if Safekeeper binary exists
    if [ ! -f "./safekeeper/safekeeper" ]; then
        log_error "Safekeeper binary not found. Building..."
        cd safekeeper
        ./build.sh
        cd ..
    fi
    
    # Clean up old test data
    rm -rf "$TEST_DATA_DIR/safekeeper-data"
    mkdir -p "$TEST_DATA_DIR/safekeeper-data"
    
    # Start Safekeeper
    log_step "Starting Safekeeper on port $SAFEKEEPER_PORT..."
    ./safekeeper/safekeeper \
        -port $SAFEKEEPER_PORT \
        -data-dir "$TEST_DATA_DIR/safekeeper-data" \
        -replica-id "safekeeper-test-1" \
        -api-key "$TEST_API_KEY" \
        > "$SAFEKEEPER_LOG" 2>&1 &
    
    SAFEKEEPER_PID=$!
    echo "Safekeeper started (PID: $SAFEKEEPER_PID)"
    
    # Wait for Safekeeper to start
    log_step "Waiting for Safekeeper to start..."
    for i in {1..10}; do
        if curl -s http://localhost:${SAFEKEEPER_PORT}/api/v1/ping > /dev/null 2>&1; then
            log_info "Safekeeper is ready"
            return 0
        fi
        sleep 1
    done
    
    log_error "Safekeeper failed to start"
    cat "$SAFEKEEPER_LOG"
    exit 1
}

# Start Page Server
start_page_server() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     Starting Page Server                                     ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    
    # Check if Page Server binary exists
    if [ ! -f "./page-server/page-server" ]; then
        log_error "Page Server binary not found. Building..."
        cd page-server
        ./build.sh
        cd ..
    fi
    
    # Clean up old test data
    rm -rf "$TEST_DATA_DIR/page-server-data"
    mkdir -p "$TEST_DATA_DIR/page-server-data"
    
    # Start Page Server
    log_step "Starting Page Server on port $PAGE_SERVER_PORT..."
    ./page-server/page-server \
        -port $PAGE_SERVER_PORT \
        -data-dir "$TEST_DATA_DIR/page-server-data" \
        -cache-size 100 \
        -api-key "$TEST_API_KEY" \
        > "$PAGE_SERVER_LOG" 2>&1 &
    
    PAGE_SERVER_PID=$!
    echo "Page Server started (PID: $PAGE_SERVER_PID)"
    
    # Wait for Page Server to start
    log_step "Waiting for Page Server to start..."
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

# Test Safekeeper endpoints
test_safekeeper_ping() {
    log_step "Testing Safekeeper ping..."
    RESPONSE=$(curl -s http://localhost:${SAFEKEEPER_PORT}/api/v1/ping)
    if echo "$RESPONSE" | grep -q '"status":"ok"'; then
        test_pass "Safekeeper ping successful"
    else
        test_fail "Safekeeper ping failed: $RESPONSE"
    fi
}

test_safekeeper_metrics() {
    log_step "Testing Safekeeper metrics..."
    RESPONSE=$(curl -s http://localhost:${SAFEKEEPER_PORT}/api/v1/metrics)
    if echo "$RESPONSE" | grep -q '"replica_id"'; then
        test_pass "Safekeeper metrics available"
        echo "  Metrics: $RESPONSE" | head -c 200
        echo "..."
    else
        test_fail "Safekeeper metrics failed: $RESPONSE"
    fi
}

test_safekeeper_wal_storage() {
    log_step "Testing WAL storage in Safekeeper..."
    
    # Create test WAL data
    WAL_DATA=$(echo -n "Test WAL Data for Safekeeper" | base64)
    
    # Stream WAL to Safekeeper
    RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${SAFEKEEPER_PORT}/api/v1/stream_wal \
        -H "Content-Type: application/json" \
        -d "{\"lsn\":1000,\"wal_data\":\"$WAL_DATA\",\"space_id\":1,\"page_no\":42}")
    
    if echo "$RESPONSE" | grep -q '"status":"success"'; then
        test_pass "WAL stored in Safekeeper successfully"
    else
        test_fail "WAL storage failed: $RESPONSE"
        return
    fi
    
    # Retrieve WAL from Safekeeper
    sleep 1
    RESPONSE=$(curl -s "http://localhost:${SAFEKEEPER_PORT}/api/v1/get_wal?lsn=1000")
    
    if echo "$RESPONSE" | grep -q '"status":"success"'; then
        test_pass "WAL retrieved from Safekeeper successfully"
    else
        test_fail "WAL retrieval failed: $RESPONSE"
    fi
}

test_safekeeper_latest_lsn() {
    log_step "Testing Safekeeper latest LSN..."
    RESPONSE=$(curl -s http://localhost:${SAFEKEEPER_PORT}/api/v1/get_latest_lsn)
    
    if echo "$RESPONSE" | grep -q '"latest_lsn"'; then
        LSN=$(echo "$RESPONSE" | grep -o '"latest_lsn":[0-9]*' | grep -o '[0-9]*')
        test_pass "Latest LSN retrieved: $LSN"
    else
        test_fail "Latest LSN retrieval failed: $RESPONSE"
    fi
}

test_safekeeper_authentication() {
    log_step "Testing Safekeeper authentication..."
    HTTP_CODE=$(curl -s -o /tmp/safekeeper_auth_test.txt -w "%{http_code}" \
        -X POST http://localhost:${SAFEKEEPER_PORT}/api/v1/stream_wal \
        -H "Content-Type: application/json" \
        -d '{"lsn":2000,"wal_data":"dGVzdA=="}')
    
    BODY=$(cat /tmp/safekeeper_auth_test.txt)
    
    if [ "$HTTP_CODE" = "401" ] || echo "$BODY" | grep -q "Authentication required"; then
        test_pass "Safekeeper authentication working (correctly blocks unauthorized)"
    else
        test_fail "Safekeeper authentication not working: HTTP $HTTP_CODE"
    fi
}

# Test Page Server endpoints
test_page_server_ping() {
    log_step "Testing Page Server ping..."
    RESPONSE=$(curl -s http://localhost:${PAGE_SERVER_PORT}/api/v1/ping)
    if echo "$RESPONSE" | grep -q '"status":"ok"'; then
        test_pass "Page Server ping successful"
    else
        test_fail "Page Server ping failed: $RESPONSE"
    fi
}

# Test full flow: WAL → Safekeeper → Page Server
test_full_wal_flow() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     Testing Full WAL Flow: Safekeeper → Page Server        ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    
    log_step "Step 1: Stream WAL to Safekeeper..."
    WAL_DATA=$(echo -n "Full Flow Test WAL Data" | base64)
    
    RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${SAFEKEEPER_PORT}/api/v1/stream_wal \
        -H "Content-Type: application/json" \
        -d "{\"lsn\":5000,\"wal_data\":\"$WAL_DATA\",\"space_id\":1,\"page_no\":100}")
    
    if echo "$RESPONSE" | grep -q '"status":"success"'; then
        test_pass "WAL streamed to Safekeeper"
    else
        test_fail "WAL streaming to Safekeeper failed: $RESPONSE"
        return
    fi
    
    sleep 1
    
    log_step "Step 2: Verify WAL in Safekeeper..."
    RESPONSE=$(curl -s "http://localhost:${SAFEKEEPER_PORT}/api/v1/get_wal?lsn=5000")
    if echo "$RESPONSE" | grep -q '"status":"success"'; then
        test_pass "WAL verified in Safekeeper"
    else
        test_fail "WAL verification in Safekeeper failed: $RESPONSE"
        return
    fi
    
    log_step "Step 3: Stream WAL from Safekeeper to Page Server..."
    # For now, we'll manually stream the WAL to Page Server
    # In production, Page Server would pull from Safekeeper
    RETRIEVED_WAL=$(echo "$RESPONSE" | grep -o '"wal_data":"[^"]*"' | cut -d'"' -f4)
    
    if [ -n "$RETRIEVED_WAL" ]; then
        RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
            -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/stream_wal \
            -H "Content-Type: application/json" \
            -d "{\"lsn\":5000,\"wal_data\":\"$RETRIEVED_WAL\",\"space_id\":1,\"page_no\":100}")
        
        if echo "$RESPONSE" | grep -q '"status":"success"'; then
            test_pass "WAL streamed to Page Server from Safekeeper"
        else
            test_fail "WAL streaming to Page Server failed: $RESPONSE"
        fi
    else
        test_fail "Could not retrieve WAL from Safekeeper"
    fi
    
    sleep 1
    
    log_step "Step 4: Verify page in Page Server..."
    RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/get_page \
        -H "Content-Type: application/json" \
        -d '{"space_id":1,"page_no":100,"lsn":5000}')
    
    if echo "$RESPONSE" | grep -q '"status":"success"'; then
        test_pass "Page retrieved from Page Server (full flow successful)"
    else
        test_fail "Page retrieval failed: $RESPONSE"
    fi
}

# Test multiple WAL records
test_multiple_wal_records() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     Testing Multiple WAL Records                            ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    
    for i in {1..5}; do
        LSN=$((6000 + i * 100))
        WAL_DATA=$(echo -n "WAL Record $i" | base64)
        
        log_step "Storing WAL record $i (LSN: $LSN)..."
        RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
            -X POST http://localhost:${SAFEKEEPER_PORT}/api/v1/stream_wal \
            -H "Content-Type: application/json" \
            -d "{\"lsn\":$LSN,\"wal_data\":\"$WAL_DATA\",\"space_id\":1,\"page_no\":$((200 + i))}")
        
        if echo "$RESPONSE" | grep -q '"status":"success"'; then
            test_pass "WAL record $i stored (LSN: $LSN)"
        else
            test_fail "WAL record $i storage failed: $RESPONSE"
        fi
    done
    
    sleep 1
    
    # Check latest LSN
    RESPONSE=$(curl -s http://localhost:${SAFEKEEPER_PORT}/api/v1/get_latest_lsn)
    LATEST_LSN=$(echo "$RESPONSE" | grep -o '"latest_lsn":[0-9]*' | grep -o '[0-9]*')
    
    if [ "$LATEST_LSN" = "6500" ]; then
        test_pass "Latest LSN correct: $LATEST_LSN"
    else
        test_fail "Latest LSN incorrect: expected 6500, got $LATEST_LSN"
    fi
}

# Test database write simulation
test_database_write_simulation() {
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     Database Write Simulation Test                           ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    
    log_step "Simulating database write: INSERT INTO test_table..."
    
    # Simulate a database write operation
    # In a real scenario, MariaDB would:
    # 1. Generate WAL record for the INSERT
    # 2. Stream WAL to Safekeeper
    # 3. Safekeeper stores and replicates
    # 4. Page Server pulls WAL and applies to pages
    
    # Step 1: Simulate WAL generation (INSERT operation)
    INSERT_WAL=$(echo -n "INSERT INTO test_table VALUES (1, 'test_data')" | base64)
    LSN=10000
    
    log_step "Step 1: Database generates WAL (LSN: $LSN)..."
    
    # Step 2: Stream WAL to Safekeeper (as MariaDB would)
    RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${SAFEKEEPER_PORT}/api/v1/stream_wal \
        -H "Content-Type: application/json" \
        -d "{\"lsn\":$LSN,\"wal_data\":\"$INSERT_WAL\",\"space_id\":1,\"page_no\":500}")
    
    if echo "$RESPONSE" | grep -q '"status":"success"'; then
        test_pass "WAL streamed to Safekeeper (database write persisted)"
    else
        test_fail "WAL streaming to Safekeeper failed: $RESPONSE"
        return
    fi
    
    sleep 1
    
    # Step 3: Verify WAL in Safekeeper (durability check)
    log_step "Step 2: Verifying WAL durability in Safekeeper..."
    RESPONSE=$(curl -s "http://localhost:${SAFEKEEPER_PORT}/api/v1/get_wal?lsn=$LSN")
    
    if echo "$RESPONSE" | grep -q '"status":"success"'; then
        test_pass "WAL durably stored in Safekeeper (write is safe)"
    else
        test_fail "WAL not found in Safekeeper: $RESPONSE"
        return
    fi
    
    # Step 4: Page Server pulls WAL from Safekeeper (simulated)
    log_step "Step 3: Page Server pulls WAL from Safekeeper..."
    RETRIEVED_WAL=$(echo "$RESPONSE" | grep -o '"wal_data":"[^"]*"' | cut -d'"' -f4)
    
    if [ -n "$RETRIEVED_WAL" ]; then
        RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
            -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/stream_wal \
            -H "Content-Type: application/json" \
            -d "{\"lsn\":$LSN,\"wal_data\":\"$RETRIEVED_WAL\",\"space_id\":1,\"page_no\":500}")
        
        if echo "$RESPONSE" | grep -q '"status":"success"'; then
            test_pass "Page Server applied WAL (write processed)"
        else
            test_fail "Page Server WAL application failed: $RESPONSE"
            return
        fi
    else
        test_fail "Could not retrieve WAL from Safekeeper"
        return
    fi
    
    sleep 1
    
    # Step 5: Verify data is readable (simulate SELECT)
    log_step "Step 4: Verifying data is readable (simulating SELECT)..."
    RESPONSE=$(curl -s -H "X-API-Key: $TEST_API_KEY" \
        -X POST http://localhost:${PAGE_SERVER_PORT}/api/v1/get_page \
        -H "Content-Type: application/json" \
        -d "{\"space_id\":1,\"page_no\":500,\"lsn\":$LSN}")
    
    if echo "$RESPONSE" | grep -q '"status":"success"'; then
        test_pass "Data is readable (database write successful)"
        echo ""
        echo "  ✅ Database write flow complete:"
        echo "     1. WAL generated (INSERT operation)"
        echo "     2. WAL stored in Safekeeper (durability)"
        echo "     3. WAL applied to Page Server (consistency)"
        echo "     4. Data is readable (verification)"
    else
        test_fail "Data not readable: $RESPONSE"
    fi
}

# Main test execution
main() {
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     Safekeeper + Page Server E2E Test Suite                 ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    echo ""
    echo "Testing the full flow:"
    echo "  1. Safekeeper WAL storage"
    echo "  2. Safekeeper → Page Server flow"
    echo "  3. Database write simulation"
    echo ""
    
    # Start services
    start_safekeeper
    start_page_server
    
    # Test Safekeeper
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     Safekeeper Tests                                         ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    
    test_safekeeper_ping
    test_safekeeper_metrics
    test_safekeeper_authentication
    test_safekeeper_wal_storage
    test_safekeeper_latest_lsn
    
    # Test Page Server
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║     Page Server Tests                                        ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    
    test_page_server_ping
    
    # Test full flow
    test_full_wal_flow
    test_multiple_wal_records
    
    # Test database write simulation
    test_database_write_simulation
    
    # Print summary
    echo ""
    echo "╔══════════════════════════════════════════════════════════════╗"
    echo "║                    Test Summary                              ║"
    echo "╚══════════════════════════════════════════════════════════════╝"
    echo ""
    echo "Tests Passed: $TESTS_PASSED"
    echo "Tests Failed: $TESTS_FAILED"
    echo ""
    
    if [ $TESTS_FAILED -eq 0 ]; then
        echo -e "${GREEN}✓ All tests passed!${NC}"
        echo ""
        echo "Safekeeper is working correctly!"
        echo "  - WAL storage: ✅"
        echo "  - WAL retrieval: ✅"
        echo "  - Full flow (Safekeeper → Page Server): ✅"
        exit 0
    else
        echo -e "${RED}✗ Some tests failed${NC}"
        exit 1
    fi
}

# Run main function
main

