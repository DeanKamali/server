# Neon Serverless Components Analysis

**Last Updated**: November 10, 2025

## Executive Summary

Based on analysis of Neon's architecture and public documentation, here are the **8 missing components** needed to make our solution fully serverless:

1. **Control Plane / API Server** ❌
2. **Compute Node Lifecycle Manager** ❌
3. **Connection Proxy / Router** ❌
4. **Suspend Scheduler** ❌
5. **Fast Resume Mechanism** ❌
6. **Usage Tracker / Billing** ❌
7. **State Store** ❌
8. **Connection Pool Manager** ❌

**Current Status**: We have the storage layer (Page Server + Safekeeper), but missing the orchestration layer.

---

## Neon's Serverless Architecture Components

Based on Neon's architecture and public information, here are the key components that enable serverless functionality:

---

## 1. Control Plane / API Server

### Purpose
- Manages compute node lifecycle
- Handles tenant/project management
- Coordinates between components
- Provides REST API for operations

### Key Responsibilities
- **Compute Node Management**
  - Create/destroy compute nodes
  - Track compute node state (active, suspended, terminated)
  - Manage compute node configuration
  
- **Project/Tenant Management**
  - Create/delete projects (databases)
  - Manage project settings
  - Handle resource quotas
  
- **Connection Routing**
  - Route connections to appropriate compute nodes
  - Handle connection pooling
  - Manage connection limits

### Implementation Approach
- **REST API** (likely Go or Rust)
- **Database** for state management (PostgreSQL)
- **Message Queue** for async operations
- **Kubernetes** or similar for orchestration

### What We Need
```go
// control-plane/cmd/api/main.go
type ControlPlane struct {
    computeManager *ComputeManager
    projectManager *ProjectManager
    connectionRouter *ConnectionRouter
    billing *BillingService
}

// Manages compute node lifecycle
type ComputeManager struct {
    kubernetesClient kubernetes.Interface
    computeNodes map[string]*ComputeNode
}

// Manages projects/tenants
type ProjectManager struct {
    db *sql.DB
    projects map[string]*Project
}
```

---

## 2. Compute Node Lifecycle Manager

### Purpose
- Creates compute nodes on-demand
- Suspends compute nodes when idle
- Resumes compute nodes on query
- Destroys compute nodes when not needed

### Key Features

#### Auto-Suspend
- **Trigger**: Compute node idle for X seconds (e.g., 5 minutes)
- **Action**: 
  - Save compute node state (memory snapshot or checkpoint)
  - Stop PostgreSQL process
  - Release resources (CPU, memory)
  - Mark as "suspended" in control plane

#### Auto-Resume
- **Trigger**: First connection/query to suspended compute node
- **Action**:
  - Restore compute node state
  - Start PostgreSQL process
  - Restore connection to Page Server
  - Mark as "active" in control plane
  - **Target**: < 1 second wake-up time

#### State Management
- **Active**: Running and serving queries
- **Suspending**: In process of suspending
- **Suspended**: Stopped, can be resumed
- **Resuming**: In process of resuming
- **Terminated**: Destroyed, cannot be resumed

### Implementation Approach
- **Kubernetes StatefulSets** or **Custom Resources**
- **VM Snapshots** (if using VMs) or **Container Checkpoints**
- **State Machine** for lifecycle transitions

### What We Need
```go
// control-plane/internal/compute/manager.go
type ComputeManager struct {
    k8sClient kubernetes.Interface
    stateStore *StateStore
}

func (cm *ComputeManager) CreateComputeNode(projectID string) (*ComputeNode, error) {
    // Create Kubernetes pod/VM
    // Configure connection to Page Server
    // Configure connection to Safekeeper
    // Start MariaDB
}

func (cm *ComputeManager) SuspendComputeNode(computeID string) error {
    // Checkpoint MariaDB state
    // Stop MariaDB process
    // Release resources
    // Update state to "suspended"
}

func (cm *ComputeManager) ResumeComputeNode(computeID string) error {
    // Restore checkpoint
    // Start MariaDB process
    // Restore connections
    // Update state to "active"
}
```

---

## 3. Connection Proxy / Router

### Purpose
- Routes client connections to compute nodes
- Handles wake-up of suspended compute nodes
- Manages connection pooling
- Provides single endpoint for clients

### Key Features

#### Connection Routing
- **Active Compute**: Route directly to compute node
- **Suspended Compute**: 
  1. Trigger resume
  2. Wait for resume completion
  3. Route connection to compute node
  4. Return connection to client

#### Connection Pooling
- Maintain pool of connections to compute nodes
- Reuse connections when possible
- Handle connection limits per compute node

#### Health Checks
- Monitor compute node health
- Route around unhealthy nodes
- Trigger recovery if needed

### Implementation Approach
- **Proxy Server** (e.g., PgBouncer-like, or custom)
- **Connection Protocol** (PostgreSQL protocol for MariaDB)
- **State Management** (track compute node states)

### What We Need
```go
// control-plane/internal/proxy/router.go
type ConnectionRouter struct {
    computeManager *ComputeManager
    activeConnections map[string][]*Connection
}

func (cr *ConnectionRouter) RouteConnection(projectID string, conn *Connection) error {
    computeNode := cr.computeManager.GetComputeNode(projectID)
    
    if computeNode.State == "suspended" {
        // Trigger resume
        if err := cr.computeManager.ResumeComputeNode(computeNode.ID); err != nil {
            return err
        }
        // Wait for resume (with timeout)
        cr.waitForResume(computeNode.ID, 5*time.Second)
    }
    
    // Route connection to compute node
    return cr.forwardConnection(computeNode, conn)
}
```

---

## 4. Idle Timeout / Suspend Scheduler

### Purpose
- Monitors compute node activity
- Triggers suspend when idle
- Manages suspend/resume timing

### Key Features

#### Activity Monitoring
- Track last query time per compute node
- Track active connections
- Track resource usage

#### Suspend Decision
- **Idle Timeout**: Default 5 minutes (configurable)
- **Conditions**: 
  - No active connections
  - No queries in last X seconds
  - Resource usage below threshold

#### Scheduling
- Periodic check (e.g., every 30 seconds)
- Batch suspend operations
- Respect suspend/resume in progress

### Implementation Approach
- **Background Worker** (goroutine or similar)
- **Timer-based** checks
- **Event-driven** (on query completion)

### What We Need
```go
// control-plane/internal/scheduler/suspend.go
type SuspendScheduler struct {
    computeManager *ComputeManager
    idleTimeout time.Duration
}

func (ss *SuspendScheduler) Start() {
    ticker := time.NewTicker(30 * time.Second)
    for range ticker.C {
        ss.checkAndSuspend()
    }
}

func (ss *SuspendScheduler) checkAndSuspend() {
    for _, compute := range ss.computeManager.GetActiveComputes() {
        if compute.LastActivity().Before(time.Now().Add(-ss.idleTimeout)) {
            if compute.ActiveConnections() == 0 {
                ss.computeManager.SuspendComputeNode(compute.ID)
            }
        }
    }
}
```

---

## 5. Billing / Usage Tracking

### Purpose
- Track compute seconds (active time)
- Track storage usage
- Generate usage reports
- Calculate costs

### Key Features

#### Compute Time Tracking
- **Start Time**: When compute node becomes active
- **End Time**: When compute node is suspended/terminated
- **Billing Granularity**: Per second (or per minute)

#### Storage Tracking
- **Page Server**: Track pages stored
- **Safekeeper**: Track WAL stored
- **S3**: Track object storage usage

#### Usage Aggregation
- Per-project usage
- Per-tenant usage
- Time-based aggregation (hourly, daily, monthly)

### Implementation Approach
- **Event Logging** (compute start/stop events)
- **Time-series Database** (for metrics)
- **Billing Service** (calculates costs)

### What We Need
```go
// control-plane/internal/billing/tracker.go
type UsageTracker struct {
    db *sql.DB
}

type ComputeUsage struct {
    ProjectID string
    ComputeID string
    StartTime time.Time
    EndTime   time.Time
    Seconds   int64
}

func (ut *UsageTracker) RecordComputeStart(projectID, computeID string) {
    // Log compute start event
}

func (ut *UsageTracker) RecordComputeStop(projectID, computeID string) {
    // Calculate seconds
    // Store usage record
}

func (ut *UsageTracker) GetUsage(projectID string, start, end time.Time) (*UsageReport, error) {
    // Aggregate usage from events
}
```

---

## 6. Compute Node State Store

### Purpose
- Persist compute node state
- Track compute node lifecycle
- Enable recovery after control plane restart

### Key Features

#### State Persistence
- Compute node configuration
- Current state (active/suspended/etc.)
- Last activity timestamp
- Resource allocation

#### Recovery
- Restore compute node states on restart
- Recover suspended compute nodes
- Handle orphaned compute nodes

### Implementation Approach
- **Database** (PostgreSQL or similar)
- **Kubernetes Custom Resources** (if using K8s)
- **Distributed Key-Value Store** (etcd, Consul)

### What We Need
```go
// control-plane/internal/state/store.go
type StateStore struct {
    db *sql.DB
}

type ComputeNodeState struct {
    ID          string
    ProjectID   string
    State       string // active, suspended, resuming, etc.
    CreatedAt   time.Time
    LastActivity time.Time
    Config      *ComputeConfig
}

func (ss *StateStore) SaveState(state *ComputeNodeState) error {
    // Persist to database
}

func (ss *StateStore) LoadState(computeID string) (*ComputeNodeState, error) {
    // Load from database
}
```

---

## 7. Fast Resume Mechanism

### Purpose
- Enable sub-second compute node wake-up
- Minimize resume latency
- Provide seamless user experience

### Key Techniques

#### Checkpoint/Restore
- **CRIU** (Checkpoint/Restore In Userspace) for containers
- **VM Snapshots** for VMs
- **Memory Snapshots** for fast restore

#### Pre-warming
- Keep minimal resources allocated
- Pre-load critical data structures
- Maintain connection pool to Page Server

#### Optimized Startup
- Skip unnecessary initialization
- Parallel startup of components
- Lazy loading of non-critical data

### Implementation Approach
- **Container Checkpoints** (Docker/Kubernetes checkpoint)
- **VM Snapshots** (if using VMs)
- **Custom Fast-Start** MariaDB configuration

### What We Need
```go
// control-plane/internal/compute/resume.go
type FastResume struct {
    checkpointDir string
}

func (fr *FastResume) CreateCheckpoint(computeID string) error {
    // Create checkpoint using CRIU or similar
    // Save to persistent storage
}

func (fr *FastResume) RestoreCheckpoint(computeID string) error {
    // Restore from checkpoint
    // Resume MariaDB process
    // Target: < 1 second
}
```

---

## Complete Architecture

```
┌─────────────────────────────────────────┐
│   Client Applications                   │
└──────────────┬────────────────────────┘
               │ PostgreSQL Protocol
               ↓
┌──────────────┴────────────────────────┐
│   Connection Proxy / Router           │
│   • Routes to compute nodes           │
│   • Triggers resume if suspended     │
│   • Connection pooling                │
└──────────────┬────────────────────────┘
               │
               ↓
┌──────────────┴────────────────────────┐
│   Control Plane / API Server          │
│   • REST API                          │
│   • Compute Node Manager              │
│   • Project Manager                   │
│   • Suspend Scheduler                 │
│   • Usage Tracker                     │
└──────────────┬────────────────────────┘
               │
               ↓
┌──────────────┴────────────────────────┐
│   Compute Node Lifecycle Manager      │
│   • Create/Destroy                    │
│   • Suspend/Resume                    │
│   • State Management                  │
└──────────────┬────────────────────────┘
               │ Kubernetes/VM Management
               ↓
┌──────────────┴────────────────────────┐
│   MariaDB Compute Nodes (Ephemeral)   │
│   • Stateless                         │
│   • Auto-suspend/resume              │
│   • On-demand creation               │
└──────────────┬────────────────────────┘
               │
               ├──→ Safekeeper (Persistent)
               └──→ Page Server (Persistent)
```

---

## Implementation Roadmap

### Phase 1: Core Control Plane (MVP)
1. **Control Plane API Server**
   - REST API for project/compute management
   - Basic compute node CRUD operations
   - State management

2. **Compute Node Manager**
   - Create/destroy compute nodes (Kubernetes)
   - Basic state tracking
   - Connection to Page Server/Safekeeper

### Phase 2: Auto-Suspend/Resume
3. **Suspend Scheduler**
   - Monitor compute node activity
   - Trigger suspend on idle timeout

4. **Fast Resume**
   - Implement checkpoint/restore
   - Optimize resume time (< 1 second)

5. **Connection Proxy**
   - Route connections
   - Handle resume on connection

### Phase 3: Production Features
6. **Usage Tracking**
   - Track compute seconds
   - Track storage usage
   - Generate reports

7. **Multi-Tenancy**
   - Resource quotas
   - Isolation
   - Billing per tenant

8. **Advanced Features**
   - Auto-scaling (multiple compute nodes per project)
   - Load balancing
   - Health monitoring and recovery

---

## Missing Components Summary

| Component | Status | Priority | Complexity |
|-----------|--------|----------|------------|
| **Control Plane API** | ❌ Missing | High | Medium |
| **Compute Node Manager** | ❌ Missing | High | High |
| **Connection Proxy** | ❌ Missing | High | Medium |
| **Suspend Scheduler** | ❌ Missing | High | Low |
| **Fast Resume** | ❌ Missing | High | High |
| **Usage Tracker** | ❌ Missing | Medium | Low |
| **State Store** | ❌ Missing | High | Low |
| **Billing System** | ❌ Missing | Low | Medium |

**Total Missing Components**: 8

**Estimated Implementation Time**: 
- Phase 1 (MVP): 2-3 months
- Phase 2 (Auto-suspend/resume): 2-3 months
- Phase 3 (Production): 3-4 months
- **Total**: 7-10 months for full serverless platform

---

## Next Steps

1. **Research Neon's Open Source Components**
   - Check if any components are open source
   - Review architecture documentation
   - Study public APIs

2. **Design Control Plane**
   - Define API endpoints
   - Design state management
   - Plan Kubernetes integration

3. **Implement MVP**
   - Basic control plane API
   - Compute node creation/destruction
   - Connection routing

4. **Add Auto-Suspend/Resume**
   - Implement suspend scheduler
   - Implement fast resume
   - Test wake-up latency

---

**Last Updated**: November 10, 2025  
**Status**: Analysis Complete - 8 Components Identified

