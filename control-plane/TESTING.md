# Control Plane Testing Guide

## Quick Start

### Prerequisites

1. **PostgreSQL Database** running and accessible
2. **Kubernetes Cluster** configured (or use `-kubeconfig` flag)
3. **Control Plane** binary built and running
4. **jq** and **curl** installed

### Start Control Plane

```bash
cd control-plane
go build -o control-plane ./cmd/api

./control-plane \
  -port 8080 \
  -db-dsn "postgres://user:password@localhost:5432/control_plane?sslmode=disable" \
  -kubeconfig ~/.kube/config \
  -namespace default \
  -idle-timeout 5m \
  -check-interval 30s
```

### Run Test Script

```bash
# From project root
./test_control_plane.sh
```

Or with custom configuration:

```bash
CONTROL_PLANE_URL=http://localhost:8080 \
PAGE_SERVER_URL=http://localhost:8081 \
SAFEKEEPER_URL=http://localhost:8082 \
./test_control_plane.sh
```

## Test Coverage

The test script covers all 4 critical components:

### 1. Control Plane API ✅
- ✅ Create project
- ✅ List projects
- ✅ Get project
- ✅ Delete project

### 2. Compute Node Manager ✅
- ✅ Create compute node
- ✅ Get compute node
- ✅ Suspend compute node
- ✅ Resume compute node
- ✅ Destroy compute node

### 3. Connection Proxy ✅
- ✅ Wake compute endpoint
- ✅ Wake compute after suspend (auto-resume)

### 4. Suspend/Resume ✅
- ✅ Suspend idle compute node
- ✅ Resume suspended compute node
- ✅ Auto-resume on wake_compute

## Manual Testing

### 1. Create Project

```bash
curl -X POST http://localhost:8080/api/v1/projects \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-project",
    "config": {
      "page_server_url": "http://localhost:8081",
      "safekeeper_url": "http://localhost:8082",
      "idle_timeout": 300,
      "max_connections": 100
    }
  }' | jq
```

### 2. Create Compute Node

```bash
PROJECT_ID="<project-id-from-step-1>"

curl -X POST "http://localhost:8080/api/v1/projects/$PROJECT_ID/compute" \
  -H "Content-Type: application/json" \
  -d '{
    "config": {
      "image": "mariadb:latest",
      "resources": {
        "cpu": "500m",
        "memory": "1Gi"
      }
    }
  }' | jq
```

### 3. Wake Compute (Proxy)

```bash
curl -X GET "http://localhost:8080/api/v1/wake_compute?endpointish=$PROJECT_ID" | jq
```

### 4. Suspend Compute Node

```bash
COMPUTE_ID="<compute-id-from-step-2>"

curl -X POST "http://localhost:8080/api/v1/compute/$COMPUTE_ID/suspend" | jq
```

### 5. Resume Compute Node

```bash
curl -X POST "http://localhost:8080/api/v1/compute/$COMPUTE_ID/resume" | jq
```

## Expected Behavior

### Auto-Suspend

1. Compute node is created and active
2. After 5 minutes of inactivity, suspend scheduler suspends it
3. Kubernetes pod is deleted
4. State changes to "suspended"

### Auto-Resume

1. Client connects to proxy
2. Proxy calls `/wake_compute?endpointish=<project-id>`
3. Control plane checks compute node state
4. If suspended, recreates Kubernetes pod
5. Waits for pod to be ready
6. Returns compute node address
7. Proxy routes connection to compute node

## Troubleshooting

### Control Plane Not Responding

```bash
# Check if control plane is running
curl http://localhost:8080/api/v1/projects

# Check logs
# (if running in foreground, check terminal output)
```

### Kubernetes Issues

```bash
# Check if Kubernetes is accessible
kubectl get pods

# Check if namespace exists
kubectl get namespace default

# Check pod status
kubectl get pods -l app=mariadb-compute
```

### Database Issues

```bash
# Check PostgreSQL connection
psql -h localhost -U postgres -d control_plane -c "SELECT COUNT(*) FROM projects;"

# Check if tables exist
psql -h localhost -U postgres -d control_plane -c "\dt"
```

## Test Output

Successful test run should show:

```
==========================================
Control Plane Serverless Test Suite
==========================================

[TEST] Creating project: test-project-1234567890
[PASS] Create project
[INFO] Project ID: abc-123-def-456

[TEST] Listing projects
[PASS] List projects

[TEST] Getting project: abc-123-def-456
[PASS] Get project
[PASS] Project name matches

[TEST] Creating compute node for project: abc-123-def-456
[PASS] Create compute node
[INFO] Compute ID: xyz-789-uvw-012
[INFO] Compute Address: 10.0.0.1:3306

[TEST] Getting compute node: xyz-789-uvw-012
[PASS] Get compute node
[INFO] Compute node state: active
[PASS] Compute node is active

[TEST] Testing wake_compute endpoint
[PASS] Wake compute
[INFO] Wake compute address: 10.0.0.1:3306
[PASS] Wake compute endpoint works

[TEST] Suspending compute node: xyz-789-uvw-012
[PASS] Suspend compute node
[INFO] Waiting for suspend to complete...
[PASS] Compute node is suspended

[TEST] Resuming compute node: xyz-789-uvw-012
[PASS] Resume compute node
[INFO] Waiting for resume to complete...
[PASS] Compute node is active after resume

[TEST] Testing wake_compute after suspend (auto-resume)
[PASS] Wake compute after suspend
[INFO] Waiting for auto-resume...
[PASS] Compute node auto-resumed successfully

[TEST] Cleaning up test resources
[PASS] Cleanup completed

==========================================
Test Summary
==========================================
Passed: 10
Failed: 0

All tests passed!
```

## Next Steps

After successful testing:

1. **Deploy to Kubernetes** - Run control plane as a Kubernetes deployment
2. **Configure Production** - Set up production database and Kubernetes cluster
3. **Monitor** - Add monitoring and alerting
4. **Scale** - Test with multiple projects and compute nodes



