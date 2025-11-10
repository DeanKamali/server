# Missing Components for Serverless Implementation

**Last Updated**: November 10, 2025

## Quick Reference

| # | Component | Priority | Complexity | Estimated Time |
|---|-----------|----------|------------|----------------|
| 1 | Control Plane API | High | Medium | 3-4 weeks |
| 2 | Compute Node Manager | High | High | 4-6 weeks |
| 3 | Connection Proxy | High | Medium | 3-4 weeks |
| 4 | Suspend Scheduler | High | Low | 1-2 weeks |
| 5 | Fast Resume | High | High | 4-6 weeks |
| 6 | Usage Tracker | Medium | Low | 2-3 weeks |
| 7 | State Store | High | Low | 1-2 weeks |
| 8 | Connection Pool | Medium | Medium | 2-3 weeks |

**Total Estimated Time**: 20-30 weeks (5-7 months)

---

## Component 1: Control Plane API Server

### What It Does
- REST API for managing projects and compute nodes
- Coordinates all serverless operations
- Provides management interface

### Key Endpoints Needed
```
POST   /api/v1/projects              # Create project
GET    /api/v1/projects              # List projects
GET    /api/v1/projects/{id}        # Get project
DELETE /api/v1/projects/{id}        # Delete project

POST   /api/v1/projects/{id}/compute # Create compute node
GET    /api/v1/compute/{id}          # Get compute node status
DELETE /api/v1/compute/{id}         # Destroy compute node
POST   /api/v1/compute/{id}/suspend # Suspend compute node
POST   /api/v1/compute/{id}/resume  # Resume compute node
```

### Implementation Structure
```
control-plane/
├── cmd/api/
│   └── main.go
├── internal/
│   ├── api/
│   │   └── handlers.go
│   ├── compute/
│   │   └── manager.go
│   └── project/
│       └── manager.go
└── go.mod
```

### Technology
- **Language**: Go
- **Framework**: Gin or Echo
- **Database**: PostgreSQL (for state)

---

## Component 2: Compute Node Lifecycle Manager

### What It Does
- Creates MariaDB compute nodes (Kubernetes pods)
- Destroys compute nodes
- Manages compute node state
- Handles suspend/resume operations

### Key Functions
```go
CreateComputeNode(projectID, config) → ComputeNode
DestroyComputeNode(computeID) → error
SuspendComputeNode(computeID) → error
ResumeComputeNode(computeID) → error
GetComputeNode(computeID) → ComputeNode
ListComputeNodes(projectID) → []ComputeNode
```

### Kubernetes Integration
- Create StatefulSet or Pod for MariaDB
- Configure environment variables:
  - `PAGE_SERVER_URL`
  - `SAFEKEEPER_URL`
  - `PROJECT_ID`
- Mount volumes (if needed for checkpoints)

### Implementation
```go
// control-plane/internal/compute/kubernetes.go
type KubernetesManager struct {
    client kubernetes.Interface
}

func (km *KubernetesManager) CreateComputeNode(projectID string, config *ComputeConfig) (*ComputeNode, error) {
    pod := &corev1.Pod{
        ObjectMeta: metav1.ObjectMeta{
            Name: fmt.Sprintf("compute-%s", projectID),
        },
        Spec: corev1.PodSpec{
            Containers: []corev1.Container{
                {
                    Name:  "mariadb",
                    Image: "mariadb:latest",
                    Env: []corev1.EnvVar{
                        {Name: "PAGE_SERVER_URL", Value: config.PageServerURL},
                        {Name: "SAFEKEEPER_URL", Value: config.SafekeeperURL},
                    },
                },
            },
        },
    }
    
    created, err := km.client.CoreV1().Pods("default").Create(context.TODO(), pod, metav1.CreateOptions{})
    // ...
}
```

---

## Component 3: Connection Proxy / Router

### What It Does
- Intercepts client connections
- Routes to appropriate compute node
- Triggers resume if compute is suspended
- Handles connection pooling

### Key Features
- **PostgreSQL Protocol** support (for MariaDB compatibility)
- **Connection Routing** based on project ID
- **Wake-on-Connect** - triggers resume on first connection
- **Connection Pooling** - reuse connections

### Implementation
```go
// control-plane/internal/proxy/router.go
type ConnectionRouter struct {
    computeManager *ComputeManager
    proxyPort      int
}

func (cr *ConnectionRouter) Start() {
    listener, _ := net.Listen("tcp", fmt.Sprintf(":%d", cr.proxyPort))
    for {
        conn, _ := listener.Accept()
        go cr.handleConnection(conn)
    }
}

func (cr *ConnectionRouter) handleConnection(clientConn net.Conn) {
    // Parse connection string to extract project ID
    projectID := cr.extractProjectID(clientConn)
    
    // Get or create compute node
    computeNode := cr.computeManager.GetComputeNode(projectID)
    
    // If suspended, resume
    if computeNode.State == "suspended" {
        cr.computeManager.ResumeComputeNode(computeNode.ID)
        cr.waitForResume(computeNode.ID, 5*time.Second)
    }
    
    // Forward connection to compute node
    computeConn, _ := net.Dial("tcp", computeNode.Address)
    go io.Copy(computeConn, clientConn)
    go io.Copy(clientConn, computeConn)
}
```

### Technology Options
- **Custom Proxy** (Go) - Full control
- **PgBouncer** - Existing solution, may need modification
- **HAProxy** - Load balancer, may need custom logic

---

## Component 4: Suspend Scheduler

### What It Does
- Monitors compute node activity
- Triggers suspend when idle
- Manages suspend timing

### Key Features
- **Activity Monitoring** - Track last query time
- **Idle Timeout** - Default 5 minutes (configurable)
- **Batch Operations** - Suspend multiple nodes efficiently

### Implementation
```go
// control-plane/internal/scheduler/suspend.go
type SuspendScheduler struct {
    computeManager *ComputeManager
    idleTimeout    time.Duration
    checkInterval  time.Duration
}

func (ss *SuspendScheduler) Start() {
    ticker := time.NewTicker(ss.checkInterval)
    for range ticker.C {
        ss.checkAndSuspend()
    }
}

func (ss *SuspendScheduler) checkAndSuspend() {
    for _, compute := range ss.computeManager.GetActiveComputes() {
        if ss.shouldSuspend(compute) {
            go ss.computeManager.SuspendComputeNode(compute.ID)
        }
    }
}

func (ss *SuspendScheduler) shouldSuspend(compute *ComputeNode) bool {
    return compute.ActiveConnections() == 0 &&
           time.Since(compute.LastActivity()) > ss.idleTimeout
}
```

---

## Component 5: Fast Resume Mechanism

### What It Does
- Resumes compute nodes quickly (< 1 second)
- Uses checkpoint/restore for fast startup
- Minimizes resume latency

### Key Techniques

#### Option 1: Container Checkpoints (CRIU)
- Use Kubernetes checkpoint API
- Save container state before suspend
- Restore from checkpoint on resume

#### Option 2: VM Snapshots
- If using VMs, create snapshot before suspend
- Restore snapshot on resume
- Faster than container checkpoints

#### Option 3: Optimized Startup
- Pre-configured MariaDB
- Skip unnecessary initialization
- Lazy loading of data

### Implementation (CRIU Approach)
```go
// control-plane/internal/compute/resume.go
type FastResume struct {
    k8sClient kubernetes.Interface
}

func (fr *FastResume) Suspend(computeID string) error {
    // Create checkpoint using Kubernetes checkpoint API
    checkpoint := &v1alpha1.Checkpoint{
        ObjectMeta: metav1.ObjectMeta{
            Name: computeID,
        },
    }
    
    // Save checkpoint
    _, err := fr.k8sClient.CheckpointV1alpha1().Checkpoints("default").Create(
        context.TODO(), checkpoint, metav1.CreateOptions{})
    
    // Stop pod
    return fr.k8sClient.CoreV1().Pods("default").Delete(
        context.TODO(), computeID, metav1.DeleteOptions{})
}

func (fr *FastResume) Resume(computeID string) error {
    // Restore from checkpoint
    // Start pod from checkpoint
    // Target: < 1 second
}
```

### Technology
- **Kubernetes Checkpoint API** (v1.27+)
- **CRIU** (Checkpoint/Restore In Userspace)
- **Alternative**: VM snapshots if using VMs

---

## Component 6: Usage Tracker / Billing

### What It Does
- Tracks compute seconds (active time)
- Tracks storage usage
- Generates usage reports
- Calculates costs

### Key Features
- **Event Logging** - Log compute start/stop events
- **Time Aggregation** - Calculate seconds per compute node
- **Storage Tracking** - Track Page Server and Safekeeper usage
- **Reporting** - Generate usage reports

### Database Schema
```sql
CREATE TABLE compute_usage (
    id UUID PRIMARY KEY,
    project_id UUID,
    compute_id UUID,
    start_time TIMESTAMP,
    end_time TIMESTAMP,
    seconds INTEGER,
    created_at TIMESTAMP
);

CREATE TABLE storage_usage (
    id UUID PRIMARY KEY,
    project_id UUID,
    storage_type VARCHAR(50), -- 'pageserver' or 'safekeeper'
    bytes BIGINT,
    recorded_at TIMESTAMP
);
```

### Implementation
```go
// control-plane/internal/billing/tracker.go
type UsageTracker struct {
    db *sql.DB
}

func (ut *UsageTracker) RecordComputeStart(projectID, computeID string) {
    ut.db.Exec(`
        INSERT INTO compute_usage (project_id, compute_id, start_time)
        VALUES ($1, $2, NOW())
    `, projectID, computeID)
}

func (ut *UsageTracker) RecordComputeStop(projectID, computeID string) {
    ut.db.Exec(`
        UPDATE compute_usage
        SET end_time = NOW(),
            seconds = EXTRACT(EPOCH FROM (NOW() - start_time))::INTEGER
        WHERE compute_id = $1 AND end_time IS NULL
    `, computeID)
}
```

---

## Component 7: State Store

### What It Does
- Persists compute node and project state
- Enables recovery after control plane restart
- Tracks compute node lifecycle

### Database Schema
```sql
CREATE TABLE projects (
    id UUID PRIMARY KEY,
    name VARCHAR(255),
    created_at TIMESTAMP,
    config JSONB
);

CREATE TABLE compute_nodes (
    id UUID PRIMARY KEY,
    project_id UUID REFERENCES projects(id),
    state VARCHAR(50), -- 'active', 'suspended', 'resuming', etc.
    created_at TIMESTAMP,
    last_activity TIMESTAMP,
    config JSONB
);
```

### Implementation
```go
// control-plane/internal/state/store.go
type StateStore struct {
    db *sql.DB
}

func (ss *StateStore) SaveComputeState(compute *ComputeNode) error {
    return ss.db.Exec(`
        INSERT INTO compute_nodes (id, project_id, state, created_at, last_activity, config)
        VALUES ($1, $2, $3, $4, $5, $6)
        ON CONFLICT (id) DO UPDATE SET
            state = EXCLUDED.state,
            last_activity = EXCLUDED.last_activity
    `, compute.ID, compute.ProjectID, compute.State, compute.CreatedAt, compute.LastActivity, compute.Config)
}
```

---

## Component 8: Connection Pool Manager

### What It Does
- Manages connection pools to compute nodes
- Reuses connections when possible
- Handles connection limits

### Key Features
- **Connection Pooling** - Reuse connections
- **Connection Limits** - Max connections per compute node
- **Health Checks** - Monitor connection health

### Implementation
```go
// control-plane/internal/proxy/pool.go
type ConnectionPool struct {
    pools map[string]*Pool // computeID -> Pool
    mu    sync.RWMutex
}

type Pool struct {
    connections chan *Connection
    maxSize     int
}

func (cp *ConnectionPool) GetConnection(computeID string) (*Connection, error) {
    cp.mu.RLock()
    pool, exists := cp.pools[computeID]
    cp.mu.RUnlock()
    
    if !exists {
        pool = cp.createPool(computeID)
    }
    
    select {
    case conn := <-pool.connections:
        return conn, nil
    default:
        return cp.createNewConnection(computeID)
    }
}
```

---

## Implementation Priority

### Phase 1: MVP (Weeks 1-8)
1. ✅ State Store (Week 1)
2. ✅ Control Plane API (Weeks 2-4)
3. ✅ Compute Node Manager (Weeks 5-8)

### Phase 2: Auto-Suspend/Resume (Weeks 9-16)
4. ✅ Suspend Scheduler (Week 9)
5. ✅ Fast Resume (Weeks 10-14)
6. ✅ Connection Proxy (Weeks 15-16)

### Phase 3: Production (Weeks 17-24)
7. ✅ Usage Tracker (Weeks 17-19)
8. ✅ Connection Pool (Weeks 20-22)
9. ✅ Testing & Optimization (Weeks 23-24)

---

## Technology Stack

| Component | Technology | Rationale |
|-----------|------------|-----------|
| **Control Plane** | Go + Gin/Echo | Matches existing codebase |
| **Kubernetes** | Kubernetes API | Industry standard |
| **Database** | PostgreSQL | Reliable, ACID |
| **Connection Proxy** | Go (custom) | Full control, performance |
| **Checkpoint** | CRIU / K8s Checkpoint | Fast resume |
| **Monitoring** | Prometheus + Grafana | Standard stack |

---

## Quick Start Guide

### Step 1: Set Up Control Plane Structure
```bash
mkdir -p control-plane/{cmd/api,internal/{api,compute,project,state,proxy,scheduler,billing},pkg/types}
cd control-plane
go mod init github.com/linux/projects/server/control-plane
```

### Step 2: Install Dependencies
```bash
go get github.com/gin-gonic/gin
go get k8s.io/client-go
go get github.com/lib/pq
```

### Step 3: Implement State Store
- Create database schema
- Implement StateStore interface
- Test persistence

### Step 4: Implement Control Plane API
- Create REST API handlers
- Implement project management
- Implement compute node management

### Step 5: Implement Kubernetes Integration
- Create Kubernetes client
- Implement pod creation/destruction
- Configure MariaDB with Page Server/Safekeeper

### Step 6: Test End-to-End
- Create project
- Create compute node
- Connect to compute node
- Verify Page Server/Safekeeper integration

---

## Success Criteria

### MVP (Phase 1)
- ✅ Can create/destroy compute nodes via API
- ✅ Compute nodes connect to Page Server
- ✅ Compute nodes stream WAL to Safekeeper
- ✅ State persists across restarts

### Auto-Suspend/Resume (Phase 2)
- ✅ Compute nodes suspend when idle
- ✅ Compute nodes resume on connection
- ✅ Resume time < 1 second

### Production (Phase 3)
- ✅ Usage tracking accurate
- ✅ Connection pooling works
- ✅ 99.9% uptime
- ✅ Sub-second resume time

---

**Last Updated**: November 10, 2025  
**Status**: 8 Components Identified - Ready for Implementation



