# Why PostgreSQL is Needed

## Purpose

PostgreSQL is used to **persistently store state** for the control plane:

1. **Projects** - Database projects/tenants configuration
2. **Compute Nodes** - Compute node state (active, suspended, etc.)
3. **State Recovery** - When control plane restarts, it needs to know what exists

## What Gets Stored

### Projects Table
- Project ID, name, configuration
- Page Server URL, Safekeeper URL
- Idle timeout, max connections

### Compute Nodes Table
- Compute node ID, project ID
- State (active, suspended, resuming, etc.)
- Address (host:port)
- Last activity timestamp
- Configuration (image, resources)

## Why Not In-Memory?

**Problem**: If control plane restarts:
- ❌ All projects lost
- ❌ All compute nodes lost
- ❌ Can't resume suspended nodes
- ❌ State inconsistency

**With PostgreSQL**:
- ✅ Projects persist across restarts
- ✅ Compute node state preserved
- ✅ Can resume suspended nodes
- ✅ Consistent state

## Neon's Approach

Neon also uses PostgreSQL for control plane state storage. They run a PostgreSQL instance specifically for the storage controller to persist:
- Tenant information
- Timeline information
- Compute node state
- Configuration

## Alternatives

### Option 1: In-Memory (Testing Only)
For testing, we could use in-memory storage, but:
- State lost on restart
- Not suitable for production

### Option 2: SQLite (Lightweight)
Could use SQLite instead:
- No separate server needed
- File-based storage
- Good for single-instance deployments

### Option 3: Keep PostgreSQL (Recommended)
- Production-ready
- ACID guarantees
- Multi-instance support
- Matches Neon's architecture

## Making It Optional

We can add an in-memory store for testing. Would you like me to:
1. Add an in-memory store option?
2. Make PostgreSQL optional for testing?
3. Add SQLite as an alternative?



