#!/bin/bash

# Full Integration Test using Docker images from stackblaze
# Tests: Control Plane + Page Server + Safekeeper + MariaDB (all as Docker containers)

set -e

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Configuration
REGISTRY="${REGISTRY:-docker.io}"
REGISTRY_USER="${REGISTRY_USER:-stackblaze}"
IMAGE_TAG="${IMAGE_TAG:-latest}"

CONTROL_PLANE_IMAGE="$REGISTRY/$REGISTRY_USER/control-plane:$IMAGE_TAG"
PAGE_SERVER_IMAGE="$REGISTRY/$REGISTRY_USER/page-server:$IMAGE_TAG"
SAFEKEEPER_IMAGE="$REGISTRY/$REGISTRY_USER/safekeeper:$IMAGE_TAG"
MARIADB_IMAGE="$REGISTRY/$REGISTRY_USER/mariadb-pageserver:$IMAGE_TAG"

# Container names
PAGE_SERVER_CONTAINER="page-server-test"
SAFEKEEPER_CONTAINER="safekeeper-test"
CONTROL_PLANE_CONTAINER="control-plane-test"

# Ports
PAGE_SERVER_PORT=8081
SAFEKEEPER_PORT=8082
CONTROL_PLANE_PORT=8080

# URLs (using container names for Docker network)
PAGE_SERVER_URL="http://$PAGE_SERVER_CONTAINER:8081"
SAFEKEEPER_URL="http://$SAFEKEEPER_CONTAINER:8082"
CONTROL_PLANE_URL="http://localhost:$CONTROL_PLANE_PORT"

# Cleanup function
cleanup() {
    echo ""
    echo -e "${YELLOW}Cleaning up...${NC}"
    
    docker stop $PAGE_SERVER_CONTAINER $SAFEKEEPER_CONTAINER $CONTROL_PLANE_CONTAINER 2>/dev/null || true
    docker rm $PAGE_SERVER_CONTAINER $SAFEKEEPER_CONTAINER $CONTROL_PLANE_CONTAINER 2>/dev/null || true
    
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
    return 1
}

# Step 1: Create Docker network
create_network() {
    print_test "Creating Docker network..."
    docker network create serverless-test 2>/dev/null || print_info "Network already exists"
    print_success "Docker network ready"
}

# Step 2: Start Page Server
start_page_server() {
    print_test "Starting Page Server container..."
    
    docker run -d \
        --name $PAGE_SERVER_CONTAINER \
        --network serverless-test \
        -p $PAGE_SERVER_PORT:8081 \
        -v page-server-data:/var/lib/page-server \
        $PAGE_SERVER_IMAGE \
        -port 8081 \
        -data-dir /var/lib/page-server
    
    wait_for_service "http://localhost:$PAGE_SERVER_PORT/api/v1/ping" "Page Server"
}

# Step 3: Start Safekeeper
start_safekeeper() {
    print_test "Starting Safekeeper container..."
    
    docker run -d \
        --name $SAFEKEEPER_CONTAINER \
        --network serverless-test \
        -p $SAFEKEEPER_PORT:8082 \
        -v safekeeper-data:/var/lib/safekeeper \
        $SAFEKEEPER_IMAGE \
        -port 8082 \
        -data-dir /var/lib/safekeeper
    
    wait_for_service "http://localhost:$SAFEKEEPER_PORT/api/v1/ping" "Safekeeper"
}

# Step 4: Start Control Plane
start_control_plane() {
    print_test "Starting Control Plane container..."
    
    # Get host IP for pods to access services
    HOST_IP=$(hostname -I 2>/dev/null | awk '{print $1}' || ip route get 8.8.8.8 2>/dev/null | awk '{print $7; exit}' || echo 'host.docker.internal')
    
    docker run -d \
        --name $CONTROL_PLANE_CONTAINER \
        --network serverless-test \
        -p $CONTROL_PLANE_PORT:8080 \
        -v control-plane-data:/var/lib/control-plane \
        -e MARIADB_PAGESERVER_IMAGE=$MARIADB_IMAGE \
        -v /home/linux/.kube/config:/root/.kube/config:ro \
        $CONTROL_PLANE_IMAGE \
        -port 8080 \
        -db-type sqlite \
        -db-dsn /var/lib/control-plane/control_plane.db
    
    wait_for_service "$CONTROL_PLANE_URL/api/v1/projects" "Control Plane"
}

# Step 5: Test Integration
test_integration() {
    print_test "Testing full integration..."
    
    # Get host IP for pods (pods need to access services on host)
    HOST_IP=$(hostname -I 2>/dev/null | awk '{print $1}' || ip route get 8.8.8.8 2>/dev/null | awk '{print $7; exit}' || echo 'host.docker.internal')
    POD_PAGE_SERVER_URL="http://${HOST_IP}:${PAGE_SERVER_PORT}"
    POD_SAFEKEEPER_URL="http://${HOST_IP}:${SAFEKEEPER_PORT}"
    
    # Create project
    print_info "Creating project..."
    PROJECT_RESPONSE=$(curl -s -X POST "$CONTROL_PLANE_URL/api/v1/projects" \
        -H "Content-Type: application/json" \
        -d "{
            \"name\": \"integration-test-docker\",
            \"config\": {
                \"page_server_url\": \"$POD_PAGE_SERVER_URL\",
                \"safekeeper_url\": \"$POD_SAFEKEEPER_URL\",
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
                \"page_server_url\": \"$POD_PAGE_SERVER_URL\",
                \"safekeeper_url\": \"$POD_SAFEKEEPER_URL\",
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
    
    # Check logs
    print_info "Checking pod logs..."
    kubectl logs $POD_NAME | tail -20
    
    print_success "Integration test completed!"
    
    # Cleanup test resources
    print_info "Cleaning up test resources..."
    curl -s -X DELETE "$CONTROL_PLANE_URL/api/v1/projects/$PROJECT_ID" > /dev/null
    print_success "Test resources cleaned up"
}

# Main execution
main() {
    echo "=========================================="
    echo "Full Integration Test (Docker Images)"
    echo "=========================================="
    echo ""
    echo "Configuration:"
    echo "  Registry: $REGISTRY/$REGISTRY_USER"
    echo "  Control Plane: $CONTROL_PLANE_IMAGE"
    echo "  Page Server: $PAGE_SERVER_IMAGE"
    echo "  Safekeeper: $SAFEKEEPER_IMAGE"
    echo "  MariaDB: $MARIADB_IMAGE"
    echo ""
    
    # Check prerequisites
    if ! command -v docker > /dev/null 2>&1; then
        print_error "docker not found. Please install Docker."
        exit 1
    fi
    
    if ! command -v kubectl > /dev/null 2>&1; then
        print_error "kubectl not found. Please install kubectl."
        exit 1
    fi
    
    if ! kubectl cluster-info > /dev/null 2>&1; then
        print_error "Kubernetes cluster not accessible. Please check kubeconfig."
        exit 1
    fi
    
    # Pull images if needed
    print_info "Pulling Docker images..."
    docker pull $PAGE_SERVER_IMAGE || print_error "Failed to pull $PAGE_SERVER_IMAGE"
    docker pull $SAFEKEEPER_IMAGE || print_error "Failed to pull $SAFEKEEPER_IMAGE"
    docker pull $CONTROL_PLANE_IMAGE || print_error "Failed to pull $CONTROL_PLANE_IMAGE"
    docker pull $MARIADB_IMAGE || print_error "Failed to pull $MARIADB_IMAGE"
    
    # Start services
    create_network
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

