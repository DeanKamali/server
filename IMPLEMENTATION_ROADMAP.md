# Serverless Implementation Roadmap

**Last Updated**: November 10, 2025

## Overview

This document outlines the implementation roadmap to make our solution fully serverless, matching Neon's capabilities.

---

## Current State

### ✅ What We Have
- **Stateless Compute**: MariaDB can run without local storage
- **Page Server**: Persistent page storage with tiered caching
- **Safekeeper**: Persistent WAL storage with 100% Neon parity
- **On-Demand Fetching**: Pages fetched when needed

### ❌ What We're Missing
- **Control Plane**: No orchestration layer
- **Auto-Suspend/Resume**: No idle compute suspension
- **Connection Proxy**: No routing/wake-up mechanism
- **Usage Tracking**: No billing system

---

## Implementation Phases

### Phase 1: Core Control Plane (MVP) - 2-3 months

#### 1.1 Control Plane API Server
**Goal**: Basic REST API for managing compute nodes and projects

**Components**:
- REST API server (Go)
- Project management (create/delete projects)
- Compute node management (create/destroy)
- State management (database)

**Files to Create**:
```
control-plane/
├── cmd/api/
│   └── main.go              # API server entry point
├── internal/
│   ├── api/
│   │   └── handlers.go      # HTTP handlers
│   ├── compute/
│   │   └── manager.go       # Compute node lifecycle
│   ├── project/
│   │   └── manager.go        # Project management
│   └── state/
│       └── store.go         # State persistence
└── pkg/
    └── types/
        └── types.go          # Shared types
```

**Key Features**:
- `POST /api/v1/projects` - Create project
- `DELETE /api/v1/projects/{id}` - Delete project
- `POST /api/v1/projects/{id}/compute` - Create compute node
- `DELETE /api/v1/compute/{id}` - Destroy compute node
- `GET /api/v1/compute/{id}/status` - Get compute status

#### 1.2 Kubernetes Integration
**Goal**: Create/destroy MariaDB compute nodes in Kubernetes

**Components**:
- Kubernetes client
- Pod/StatefulSet management
- Configuration management

**Implementation**:
```go
// control-plane/internal/compute/kubernetes.go
type KubernetesManager struct {
    client kubernetes.Interface
}

func (km *KubernetesManager) CreateComputeNode(projectID string, config *ComputeConfig) (*ComputeNode, error) {
    // Create Kubernetes pod with MariaDB
    // Configure connection to Page Server
    // Configure connection to Safekeeper
    // Set environment variables
}

func (km *KubernetesManager) DestroyComputeNode(computeID string) error {
    // Delete Kubernetes pod
    // Clean up resources
}
```

#### 1.3 State Management
**Goal**: Persist compute node and project state

**Database Schema**:
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
    state VARCHAR(50), -- active, suspended, resuming, etc.
    created_at TIMESTAMP,
    last_activity TIMESTAMP,
    config JSONB
);
```

---

### Phase 2: Auto-Suspend/Resume - 2-3 months

#### 2.1 Suspend Scheduler
**Goal**: Automatically suspend idle compute nodes

**Components**:
- Background worker
- Activity monitoring
- Suspend triggers

**Implementation**:
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
            ss.computeManager.SuspendComputeNode(compute.ID)
        }
    }
}

func (ss *SuspendScheduler) shouldSuspend(compute *ComputeNode) bool {
    return compute.ActiveConnections() == 0 &&
           time.Since(compute.LastActivity()) > ss.idleTimeout
}
```

#### 2.2 Fast Resume
**Goal**: Resume compute nodes in < 1 second

**Components**:
- Checkpoint/restore mechanism
- Optimized startup
- State restoration

**Implementation Options**:
1. **Container Checkpoints** (CRIU)
   - Use Kubernetes checkpoint API
   - Save/restore container state
   
2. **VM Snapshots** (if using VMs)
   - Create VM snapshot before suspend
   - Restore snapshot on resume
   
3. **Optimized Startup**
   - Pre-configured MariaDB
   - Skip unnecessary initialization
   - Lazy loading

**Implementation**:
```go
// control-plane/internal/compute/resume.go
type FastResume struct {
    checkpointDir string
    k8sClient     kubernetes.Interface
}

func (fr *FastResume) Suspend(computeID string) error {
    // Create checkpoint
    checkpoint, err := fr.createCheckpoint(computeID)
    if err != nil {
        return err
    }
    
    // Save checkpoint to persistent storage
    return fr.saveCheckpoint(computeID, checkpoint)
}

func (fr *FastResume) Resume(computeID string) error {
    // Load checkpoint
    checkpoint, err := fr.loadCheckpoint(computeID)
    if err != nil {
        return err
    }
    
    // Restore from checkpoint
    return fr.restoreCheckpoint(computeID, checkpoint)
}
```

#### 2.3 Connection Proxy
**Goal**: Route connections and trigger resume

**Components**:
- Proxy server (PostgreSQL protocol)
- Connection routing
- Resume trigger

**Implementation**:
```go
// control-plane/internal/proxy/router.go
type ConnectionRouter struct {
    computeManager *ComputeManager
    proxyServer    *ProxyServer
}

func (cr *ConnectionRouter) HandleConnection(conn *Connection) error {
    projectID := cr.extractProjectID(conn)
    computeNode := cr.computeManager.GetComputeNode(projectID)
    
    // Check if suspended
    if computeNode.State == "suspended" {
        // Trigger resume
        if err := cr.computeManager.ResumeComputeNode(computeNode.ID); err != nil {
            return err
        }
        
        // Wait for resume (with timeout)
        if err := cr.waitForResume(computeNode.ID, 5*time.Second); err != nil {
            return err
        }
    }
    
    // Route connection to compute node
    return cr.forwardConnection(computeNode, conn)
}
```

---

### Phase 3: Production Features - 3-4 months

#### 3.1 Usage Tracking
**Goal**: Track compute and storage usage for billing

**Components**:
- Event logging
- Usage aggregation
- Reporting

**Implementation**:
```go
// control-plane/internal/billing/tracker.go
type UsageTracker struct {
    db *sql.DB
}

func (ut *UsageTracker) RecordComputeStart(projectID, computeID string) {
    // Log start event
}

func (ut *UsageTracker) RecordComputeStop(projectID, computeID string) {
    // Calculate seconds
    // Store usage
}

func (ut *UsageTracker) GetUsage(projectID string, start, end time.Time) (*UsageReport, error) {
    // Aggregate usage
}
```

#### 3.2 Multi-Tenancy
**Goal**: Support multiple tenants with isolation

**Components**:
- Resource quotas
- Isolation (network, storage)
- Billing per tenant

#### 3.3 Advanced Features
**Goal**: Production-grade features

**Components**:
- Auto-scaling (multiple compute nodes)
- Load balancing
- Health monitoring
- Disaster recovery

---

## Technology Stack

### Control Plane
- **Language**: Go (matches our existing codebase)
- **Framework**: Gin or Echo (REST API)
- **Database**: PostgreSQL (for state management)
- **Orchestration**: Kubernetes (for compute nodes)

### Connection Proxy
- **Language**: Go or Rust (performance-critical)
- **Protocol**: PostgreSQL protocol (for MariaDB compatibility)
- **Library**: pgx or similar

### Fast Resume
- **Technology**: 
  - Kubernetes Checkpoint API (containers)
  - VM Snapshots (if using VMs)
  - CRIU (Checkpoint/Restore In Userspace)

---

## Estimated Timeline

| Phase | Duration | Components |
|-------|----------|------------|
| **Phase 1** | 2-3 months | Control Plane API, Kubernetes Integration, State Management |
| **Phase 2** | 2-3 months | Suspend Scheduler, Fast Resume, Connection Proxy |
| **Phase 3** | 3-4 months | Usage Tracking, Multi-Tenancy, Advanced Features |
| **Total** | **7-10 months** | Full serverless platform |

---

## Quick Start (MVP)

### Step 1: Create Control Plane Structure
```bash
mkdir -p control-plane/{cmd/api,internal/{api,compute,project,state},pkg/types}
```

### Step 2: Implement Basic API
- Project CRUD
- Compute node CRUD
- State management

### Step 3: Kubernetes Integration
- Create MariaDB pods
- Configure Page Server/Safekeeper connections
- Manage pod lifecycle

### Step 4: Test
- Create project
- Create compute node
- Connect to compute node
- Verify Page Server/Safekeeper integration

---

## Key Design Decisions

### 1. Kubernetes vs Custom Orchestration
**Decision**: Use Kubernetes
**Rationale**: 
- Industry standard
- Rich ecosystem
- Built-in features (scaling, health checks)
- Easier to maintain

### 2. Checkpoint vs Fresh Start
**Decision**: Hybrid approach
**Rationale**:
- Checkpoint for fast resume (< 1 second)
- Fresh start as fallback
- Optimize based on usage patterns

### 3. Connection Protocol
**Decision**: PostgreSQL protocol
**Rationale**:
- MariaDB compatible
- Standard protocol
- Existing libraries available

---

## Success Metrics

### Phase 1 (MVP)
- ✅ Can create/destroy compute nodes
- ✅ Can connect to compute nodes
- ✅ Compute nodes fetch pages from Page Server
- ✅ Compute nodes stream WAL to Safekeeper

### Phase 2 (Auto-Suspend/Resume)
- ✅ Compute nodes suspend when idle
- ✅ Compute nodes resume on connection
- ✅ Resume time < 1 second (target: 500ms)

### Phase 3 (Production)
- ✅ Usage tracking accurate
- ✅ Multi-tenant isolation
- ✅ 99.9% uptime
- ✅ Sub-second resume time

---

**Last Updated**: November 10, 2025  
**Status**: Roadmap Defined - Ready for Implementation



