# Full Integration Test Guide

## Overview

This guide shows how to test the complete serverless stack:
- **Control Plane**: Orchestrates compute nodes
- **Page Server**: Stores and serves database pages
- **Safekeeper**: Stores WAL records with consensus
- **MariaDB Compute Node**: Custom patched MariaDB image from `stackblaze/mariadb-pageserver:latest`

## Prerequisites

1. **k3s/Kubernetes** running and accessible
2. **Page Server** binary built (`./page-server/page-server`)
3. **Safekeeper** binary built (`./safekeeper/safekeeper`)
4. **Control Plane** binary built (`./control-plane/control-plane`)
5. **Docker image** pushed: `stackblaze/mariadb-pageserver:latest`

## Quick Start

### Option 1: Automated Test Script

```bash
# Run the full integration test
./test_full_integration.sh
```

This script will:
1. Start Page Server on port 8081
2. Start Safekeeper on port 8082
3. Start Control Plane on port 8080
4. Create a project
5. Create a compute node with the custom MariaDB image
6. Verify all components are working together

### Option 2: Manual Testing

#### Step 1: Start Page Server

```bash
cd page-server
./page-server -port 8081 -data-dir ./page-server-data
```

#### Step 2: Start Safekeeper

```bash
cd safekeeper
./safekeeper -port 8082 -data-dir ./safekeeper-data
```

#### Step 3: Start Control Plane

```bash
export MARIADB_PAGESERVER_IMAGE=stackblaze/mariadb-pageserver:latest
cd control-plane
./control-plane -port 8080 -db-type sqlite
```

#### Step 4: Create Project

```bash
curl -X POST http://localhost:8080/api/v1/projects \
  -H "Content-Type: application/json" \
  -d '{
    "name": "test-project",
    "config": {
      "page_server_url": "http://localhost:8081",
      "safekeeper_url": "http://localhost:8082",
      "idle_timeout": 300,
      "max_connections": 100
    }
  }' | jq
```

#### Step 5: Create Compute Node

```bash
# Get project ID from previous response
PROJECT_ID="<project-id>"

curl -X POST "http://localhost:8080/api/v1/projects/$PROJECT_ID/compute" \
  -H "Content-Type: application/json" \
  -d '{
    "config": {
      "image": "stackblaze/mariadb-pageserver:latest",
      "page_server_url": "http://localhost:8081",
      "safekeeper_url": "http://localhost:8082",
      "resources": {
        "cpu": "100m",
        "memory": "256Mi"
      }
    }
  }' | jq
```

#### Step 6: Verify Pod

```bash
# List pods
kubectl get pods -l app=mariadb-compute

# Check pod logs
kubectl logs <pod-name>

# Check if MariaDB is connecting to Page Server/Safekeeper
kubectl logs <pod-name> | grep -i "page.server\|safekeeper"
```

## Configuration

### Environment Variables

```bash
# Use custom MariaDB image
export MARIADB_PAGESERVER_IMAGE=stackblaze/mariadb-pageserver:latest

# Or use a different tag
export MARIADB_PAGESERVER_IMAGE=stackblaze/mariadb-pageserver:v1.0.0
```

### Service URLs

The control plane passes these URLs to the MariaDB container as environment variables:
- `PAGE_SERVER_URL`: Page Server endpoint (default: `http://localhost:8081`)
- `SAFEKEEPER_URL`: Safekeeper endpoint (default: `http://localhost:8082`)

**Important**: In Kubernetes, use service names or cluster IPs, not `localhost`:
- `http://page-server:8081` (if Page Server is a Kubernetes service)
- `http://safekeeper:8082` (if Safekeeper is a Kubernetes service)

## Troubleshooting

### Pod Not Starting

```bash
# Check pod status
kubectl describe pod <pod-name>

# Check pod logs
kubectl logs <pod-name>

# Check events
kubectl get events --sort-by='.lastTimestamp'
```

### Image Pull Errors

```bash
# Verify image exists
docker pull stackblaze/mariadb-pageserver:latest

# Check if image is accessible from k3s
k3s ctr images ls | grep mariadb-pageserver
```

### MariaDB Not Connecting to Page Server/Safekeeper

1. **Check environment variables in pod:**
   ```bash
   kubectl exec <pod-name> -- env | grep -E "PAGE_SERVER|SAFEKEEPER"
   ```

2. **Check network connectivity:**
   ```bash
   kubectl exec <pod-name> -- curl -s http://localhost:8081/health
   ```

3. **Check MariaDB logs:**
   ```bash
   kubectl logs <pod-name> | grep -i "page.server\|safekeeper\|error"
   ```

### Page Server/Safekeeper Not Accessible

If services are running on the host but not accessible from pods:

1. **Use host networking** (for local testing):
   ```yaml
   # In pod spec
   hostNetwork: true
   ```

2. **Or use NodePort/LoadBalancer** services in Kubernetes

3. **Or use the host's IP address** instead of localhost

## Expected Behavior

1. **Pod Creation**: MariaDB pod should start within 1-2 minutes
2. **Page Server Connection**: MariaDB should connect to Page Server on startup
3. **Safekeeper Connection**: MariaDB should stream WAL to Safekeeper
4. **Database Ready**: MariaDB should be ready to accept connections

## Verification

### Check Page Server Metrics

```bash
curl http://localhost:8081/metrics | grep -i "page\|request"
```

### Check Safekeeper Metrics

```bash
curl http://localhost:8082/metrics | grep -i "wal\|request"
```

### Check MariaDB Connection

```bash
# Get pod IP
POD_IP=$(kubectl get pod <pod-name> -o jsonpath='{.status.podIP}')

# Connect to MariaDB
mysql -h $POD_IP -u root -proot -e "SELECT @@version;"
```

## Next Steps

After successful integration:

1. **Test database operations**: Create tables, insert data
2. **Test WAL streaming**: Verify WAL records reach Safekeeper
3. **Test page fetching**: Verify pages are fetched from Page Server
4. **Test suspend/resume**: Test compute node lifecycle
5. **Test scaling**: Create multiple compute nodes

## Summary

The integration test verifies:
- ✅ Control Plane can create projects and compute nodes
- ✅ Custom MariaDB image is pulled and started
- ✅ MariaDB container receives Page Server and Safekeeper URLs
- ✅ All services are running and accessible
- ✅ Pod becomes ready in Kubernetes

For production, you'll want to:
- Deploy Page Server and Safekeeper as Kubernetes services
- Use proper service discovery (DNS)
- Configure TLS/HTTPS
- Set up monitoring and alerting

