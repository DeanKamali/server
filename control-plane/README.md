# Control Plane - Serverless Orchestration Layer

This is the control plane implementation for managing serverless MariaDB compute nodes, based on Neon's architecture.

## Components

### 1. Control Plane API Server ✅
- REST API for managing projects and compute nodes
- Endpoints for create/read/update/delete operations
- Wake compute endpoint for proxy integration

### 2. Compute Node Manager ✅
- Kubernetes integration for compute node lifecycle
- Create/destroy MariaDB pods
- Suspend/resume compute nodes
- State management

### 3. Connection Proxy ✅
- Routes client connections to compute nodes
- Triggers wake_compute on connection
- Handles suspended compute node resume

### 4. Suspend/Resume Scheduler ✅
- Monitors compute node activity
- Auto-suspends idle nodes (default: 5 minutes)
- Background worker for periodic checks

## Architecture

```
┌─────────────────────────────────────────┐
│   Client Applications                   │
└──────────────┬────────────────────────┘
               │ MySQL Protocol
               ↓
┌──────────────┴────────────────────────┐
│   Connection Proxy                    │
│   • Routes to compute nodes           │
│   • Triggers wake_compute            │
└──────────────┬────────────────────────┘
               │
               ↓
┌──────────────┴────────────────────────┐
│   Control Plane API                   │
│   • REST API                          │
│   • Compute Node Manager              │
│   • Suspend Scheduler                 │
└──────────────┬────────────────────────┘
               │
               ↓
┌──────────────┴────────────────────────┐
│   Kubernetes                          │
│   • MariaDB Pods                     │
│   • Lifecycle Management             │
└──────────────┬────────────────────────┘
               │
               ├──→ Safekeeper (Persistent)
               └──→ Page Server (Persistent)
```

## Setup

### Prerequisites

1. **PostgreSQL Database** (for state storage)
   ```bash
   createdb control_plane
   ```

2. **Kubernetes Cluster** (for compute nodes)
   - Access to Kubernetes cluster
   - `kubectl` configured

3. **Go 1.21+**

### Installation

```bash
cd control-plane
go mod download
go build -o control-plane ./cmd/api
```

### Configuration

```bash
./control-plane \
  -port 8080 \
  -db-dsn "postgres://user:pass@localhost:5432/control_plane?sslmode=disable" \
  -kubeconfig ~/.kube/config \
  -namespace default \
  -idle-timeout 5m \
  -check-interval 30s
```

## API Endpoints

### Projects

- `POST /api/v1/projects` - Create project
- `GET /api/v1/projects` - List projects
- `GET /api/v1/projects/:id` - Get project
- `DELETE /api/v1/projects/:id` - Delete project

### Compute Nodes

- `POST /api/v1/projects/:id/compute` - Create compute node
- `GET /api/v1/compute/:id` - Get compute node
- `DELETE /api/v1/compute/:id` - Destroy compute node
- `POST /api/v1/compute/:id/suspend` - Suspend compute node
- `POST /api/v1/compute/:id/resume` - Resume compute node

### Wake Compute (Proxy)

- `GET /api/v1/wake_compute?endpointish=<project-id>` - Wake compute node

## Usage Example

### 1. Create Project

```bash
curl -X POST http://localhost:8080/api/v1/projects \
  -H "Content-Type: application/json" \
  -d '{
    "name": "my-project",
    "config": {
      "page_server_url": "http://page-server:8080",
      "safekeeper_url": "http://safekeeper:8080",
      "idle_timeout": 300,
      "max_connections": 100
    }
  }'
```

### 2. Create Compute Node

```bash
curl -X POST http://localhost:8080/api/v1/projects/<project-id>/compute \
  -H "Content-Type: application/json" \
  -d '{
    "config": {
      "image": "mariadb:latest",
      "resources": {
        "cpu": "500m",
        "memory": "1Gi"
      }
    }
  }'
```

### 3. Connect via Proxy

The connection proxy will automatically:
1. Extract project ID from connection
2. Call `wake_compute` if needed
3. Resume suspended compute nodes
4. Route connection to compute node

## How It Works

### Auto-Suspend

1. **Suspend Scheduler** runs every 30 seconds (configurable)
2. Checks all active compute nodes
3. If idle for > 5 minutes (configurable), suspends the node
4. Deletes Kubernetes pod to free resources

### Auto-Resume

1. **Client connects** to proxy
2. **Proxy calls** `/wake_compute` endpoint
3. **Control Plane** checks compute node state
4. If **suspended**, recreates Kubernetes pod
5. Waits for pod to be ready (< 1 second target)
6. Routes connection to compute node

### State Management

- **PostgreSQL** stores project and compute node state
- **Kubernetes** manages actual compute node pods
- **State sync** ensures consistency

## Based on Neon's Architecture

This implementation follows Neon's patterns:

1. **Control Plane API** - Similar to Neon's `/proxy_wake_compute`
2. **Compute Manager** - Kubernetes pod lifecycle (like Neon's VM/compute management)
3. **Connection Proxy** - Routes connections and triggers wake (like Neon's proxy)
4. **Suspend Scheduler** - Auto-suspend idle nodes (like Neon's idle timeout)

## Next Steps

- [ ] Implement MySQL protocol parsing in proxy
- [ ] Add connection pooling
- [ ] Implement fast resume with checkpoints
- [ ] Add usage tracking/billing
- [ ] Add multi-tenancy support



