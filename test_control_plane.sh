#!/bin/bash

# Test script for Control Plane Serverless Implementation
# Tests all 4 critical components: Control Plane API, Compute Manager, Connection Proxy, Suspend/Resume

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Configuration
CONTROL_PLANE_URL="${CONTROL_PLANE_URL:-http://localhost:8080}"
PROJECT_NAME="test-project-$(date +%s)"
PAGE_SERVER_URL="${PAGE_SERVER_URL:-http://localhost:8081}"
SAFEKEEPER_URL="${SAFEKEEPER_URL:-http://localhost:8082}"

# Test counters
TESTS_PASSED=0
TESTS_FAILED=0

# Helper functions
print_test() {
    echo -e "${BLUE}[TEST]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[PASS]${NC} $1"
    ((TESTS_PASSED++))
}

print_error() {
    echo -e "${RED}[FAIL]${NC} $1"
    ((TESTS_FAILED++))
}

print_info() {
    echo -e "${YELLOW}[INFO]${NC} $1"
}

check_response() {
    local response="$1"
    local expected_status="$2"
    local test_name="$3"
    
    if echo "$response" | grep -q "\"error\""; then
        print_error "$test_name: $(echo "$response" | jq -r '.error' 2>/dev/null || echo "$response")"
        return 1
    fi
    
    if [ -n "$expected_status" ]; then
        local status=$(echo "$response" | jq -r '.status // empty' 2>/dev/null)
        if [ "$status" != "$expected_status" ]; then
            print_error "$test_name: Expected status $expected_status, got $status"
            return 1
        fi
    fi
    
    print_success "$test_name"
    return 0
}

# Wait for control plane to be ready
wait_for_control_plane() {
    print_info "Waiting for control plane to be ready..."
    local max_attempts=30
    local attempt=0
    
    while [ $attempt -lt $max_attempts ]; do
        if curl -s -f "$CONTROL_PLANE_URL/api/v1/projects" > /dev/null 2>&1; then
            print_success "Control plane is ready"
            return 0
        fi
        attempt=$((attempt + 1))
        sleep 1
    done
    
    print_error "Control plane is not responding after $max_attempts attempts"
    return 1
}

# Test 1: Create Project
test_create_project() {
    print_test "Creating project: $PROJECT_NAME"
    
    local response=$(curl -s -X POST "$CONTROL_PLANE_URL/api/v1/projects" \
        -H "Content-Type: application/json" \
        -d "{
            \"name\": \"$PROJECT_NAME\",
            \"config\": {
                \"page_server_url\": \"$PAGE_SERVER_URL\",
                \"safekeeper_url\": \"$SAFEKEEPER_URL\",
                \"idle_timeout\": 300,
                \"max_connections\": 100
            }
        }")
    
    if check_response "$response" "" "Create project"; then
        PROJECT_ID=$(echo "$response" | jq -r '.id')
        print_info "Project ID: $PROJECT_ID"
        export PROJECT_ID
        return 0
    fi
    return 1
}

# Test 2: List Projects
test_list_projects() {
    print_test "Listing projects"
    
    local response=$(curl -s -X GET "$CONTROL_PLANE_URL/api/v1/projects")
    
    if echo "$response" | jq -e '. | length >= 1' > /dev/null 2>&1; then
        print_success "List projects"
        return 0
    else
        print_error "List projects: Invalid response"
        return 1
    fi
}

# Test 3: Get Project
test_get_project() {
    print_test "Getting project: $PROJECT_ID"
    
    if [ -z "$PROJECT_ID" ]; then
        print_error "Get project: PROJECT_ID not set"
        return 1
    fi
    
    local response=$(curl -s -X GET "$CONTROL_PLANE_URL/api/v1/projects/$PROJECT_ID")
    
    if check_response "$response" "" "Get project"; then
        local name=$(echo "$response" | jq -r '.name')
        if [ "$name" = "$PROJECT_NAME" ]; then
            print_success "Project name matches"
        else
            print_error "Project name mismatch: expected $PROJECT_NAME, got $name"
            return 1
        fi
        return 0
    fi
    return 1
}

# Test 4: Create Compute Node
test_create_compute_node() {
    print_test "Creating compute node for project: $PROJECT_ID"
    
    if [ -z "$PROJECT_ID" ]; then
        print_error "Create compute node: PROJECT_ID not set"
        return 1
    fi
    
    local response=$(curl -s -X POST "$CONTROL_PLANE_URL/api/v1/projects/$PROJECT_ID/compute" \
        -H "Content-Type: application/json" \
        -d '{
            "config": {
                "image": "mariadb:latest",
                "resources": {
                    "cpu": "500m",
                    "memory": "1Gi"
                }
            }
        }')
    
    if check_response "$response" "" "Create compute node"; then
        COMPUTE_ID=$(echo "$response" | jq -r '.id')
        COMPUTE_ADDRESS=$(echo "$response" | jq -r '.address')
        print_info "Compute ID: $COMPUTE_ID"
        print_info "Compute Address: $COMPUTE_ADDRESS"
        export COMPUTE_ID
        export COMPUTE_ADDRESS
        return 0
    fi
    return 1
}

# Test 5: Get Compute Node
test_get_compute_node() {
    print_test "Getting compute node: $COMPUTE_ID"
    
    if [ -z "$COMPUTE_ID" ]; then
        print_error "Get compute node: COMPUTE_ID not set"
        return 1
    fi
    
    local response=$(curl -s -X GET "$CONTROL_PLANE_URL/api/v1/compute/$COMPUTE_ID")
    
    if check_response "$response" "" "Get compute node"; then
        local state=$(echo "$response" | jq -r '.state')
        print_info "Compute node state: $state"
        if [ "$state" = "active" ]; then
            print_success "Compute node is active"
        else
            print_error "Compute node is not active: $state"
            return 1
        fi
        return 0
    fi
    return 1
}

# Test 6: Wake Compute (Proxy Endpoint)
test_wake_compute() {
    print_test "Testing wake_compute endpoint"
    
    if [ -z "$PROJECT_ID" ]; then
        print_error "Wake compute: PROJECT_ID not set"
        return 1
    fi
    
    local response=$(curl -s -X GET "$CONTROL_PLANE_URL/api/v1/wake_compute?endpointish=$PROJECT_ID")
    
    if check_response "$response" "" "Wake compute"; then
        local address=$(echo "$response" | jq -r '.address')
        local compute_id=$(echo "$response" | jq -r '.aux.compute_id')
        print_info "Wake compute address: $address"
        print_info "Wake compute ID: $compute_id"
        print_success "Wake compute endpoint works"
        return 0
    fi
    return 1
}

# Test 7: Suspend Compute Node
test_suspend_compute_node() {
    print_test "Suspending compute node: $COMPUTE_ID"
    
    if [ -z "$COMPUTE_ID" ]; then
        print_error "Suspend compute node: COMPUTE_ID not set"
        return 1
    fi
    
    local response=$(curl -s -X POST "$CONTROL_PLANE_URL/api/v1/compute/$COMPUTE_ID/suspend")
    
    if check_response "$response" "" "Suspend compute node"; then
        print_info "Waiting for suspend to complete..."
        sleep 5
        
        # Verify state is suspended
        local get_response=$(curl -s -X GET "$CONTROL_PLANE_URL/api/v1/compute/$COMPUTE_ID")
        local state=$(echo "$get_response" | jq -r '.state')
        
        if [ "$state" = "suspended" ]; then
            print_success "Compute node is suspended"
            return 0
        else
            print_error "Compute node is not suspended: $state"
            return 1
        fi
    fi
    return 1
}

# Test 8: Resume Compute Node
test_resume_compute_node() {
    print_test "Resuming compute node: $COMPUTE_ID"
    
    if [ -z "$COMPUTE_ID" ]; then
        print_error "Resume compute node: COMPUTE_ID not set"
        return 1
    fi
    
    local response=$(curl -s -X POST "$CONTROL_PLANE_URL/api/v1/compute/$COMPUTE_ID/resume")
    
    if check_response "$response" "" "Resume compute node"; then
        print_info "Waiting for resume to complete..."
        sleep 10
        
        # Verify state is active
        local get_response=$(curl -s -X GET "$CONTROL_PLANE_URL/api/v1/compute/$COMPUTE_ID")
        local state=$(echo "$get_response" | jq -r '.state')
        
        if [ "$state" = "active" ]; then
            print_success "Compute node is active after resume"
            return 0
        else
            print_error "Compute node is not active after resume: $state"
            return 1
        fi
    fi
    return 1
}

# Test 9: Wake Compute After Suspend (Auto-Resume)
test_wake_compute_after_suspend() {
    print_test "Testing wake_compute after suspend (auto-resume)"
    
    if [ -z "$PROJECT_ID" ]; then
        print_error "Wake compute after suspend: PROJECT_ID not set"
        return 1
    fi
    
    # Suspend first
    print_info "Suspending compute node..."
    curl -s -X POST "$CONTROL_PLANE_URL/api/v1/compute/$COMPUTE_ID/suspend" > /dev/null
    sleep 5
    
    # Wake compute (should auto-resume)
    print_info "Calling wake_compute (should trigger resume)..."
    local response=$(curl -s -X GET "$CONTROL_PLANE_URL/api/v1/wake_compute?endpointish=$PROJECT_ID")
    
    if check_response "$response" "" "Wake compute after suspend"; then
        print_info "Waiting for auto-resume..."
        sleep 10
        
        # Verify state is active
        local get_response=$(curl -s -X GET "$CONTROL_PLANE_URL/api/v1/compute/$COMPUTE_ID")
        local state=$(echo "$get_response" | jq -r '.state')
        
        if [ "$state" = "active" ]; then
            print_success "Compute node auto-resumed successfully"
            return 0
        else
            print_error "Compute node did not auto-resume: $state"
            return 1
        fi
    fi
    return 1
}

# Test 10: Cleanup
test_cleanup() {
    print_test "Cleaning up test resources"
    
    if [ -n "$COMPUTE_ID" ]; then
        print_info "Destroying compute node: $COMPUTE_ID"
        curl -s -X DELETE "$CONTROL_PLANE_URL/api/v1/compute/$COMPUTE_ID" > /dev/null
    fi
    
    if [ -n "$PROJECT_ID" ]; then
        print_info "Deleting project: $PROJECT_ID"
        curl -s -X DELETE "$CONTROL_PLANE_URL/api/v1/projects/$PROJECT_ID" > /dev/null
    fi
    
    print_success "Cleanup completed"
    return 0
}

# Main test execution
main() {
    echo "=========================================="
    echo "Control Plane Serverless Test Suite"
    echo "=========================================="
    echo ""
    echo "Configuration:"
    echo "  Control Plane URL: $CONTROL_PLANE_URL"
    echo "  Page Server URL: $PAGE_SERVER_URL"
    echo "  Safekeeper URL: $SAFEKEEPER_URL"
    echo ""
    
    # Check dependencies
    if ! command -v curl &> /dev/null; then
        print_error "curl is required but not installed"
        exit 1
    fi
    
    if ! command -v jq &> /dev/null; then
        print_error "jq is required but not installed"
        exit 1
    fi
    
    # Wait for control plane
    if ! wait_for_control_plane; then
        print_error "Control plane is not available"
        exit 1
    fi
    
    echo ""
    echo "Running tests..."
    echo ""
    
    # Run tests
    test_create_project && \
    test_list_projects && \
    test_get_project && \
    test_create_compute_node && \
    test_get_compute_node && \
    test_wake_compute && \
    test_suspend_compute_node && \
    test_resume_compute_node && \
    test_wake_compute_after_suspend
    
    # Cleanup
    echo ""
    test_cleanup
    
    # Summary
    echo ""
    echo "=========================================="
    echo "Test Summary"
    echo "=========================================="
    echo -e "${GREEN}Passed: $TESTS_PASSED${NC}"
    echo -e "${RED}Failed: $TESTS_FAILED${NC}"
    echo ""
    
    if [ $TESTS_FAILED -eq 0 ]; then
        echo -e "${GREEN}All tests passed!${NC}"
        exit 0
    else
        echo -e "${RED}Some tests failed!${NC}"
        exit 1
    fi
}

# Run main
main



