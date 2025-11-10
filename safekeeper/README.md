# Safekeeper - WAL Storage with Consensus

## Overview

Safekeeper is a distributed WAL (Write-Ahead Log) storage component that ensures durability and consistency through quorum-based consensus. It acts as an intermediary between MariaDB compute nodes and the Page Server, providing guaranteed WAL persistence before acknowledging transactions.

## Architecture

```
┌─────────────────────────────────────────┐
│   MariaDB Compute Node                  │
│   • Streams WAL records                  │
└──────────────┬────────────────────────┘
               │ HTTP/JSON
               ↓
┌──────────────┴────────────────────────┐
│   Safekeeper (Multiple Replicas)     │
│   • Quorum-based consensus           │
│   • WAL replication                   │
│   • Durability guarantees            │
└──────────────┬───────────────────────┘
               │ WAL Pull
               ↓
┌──────────────┴────────────────────────┐
│   Page Server                         │
│   • Pulls WAL from Safekeeper        │
│   • Applies WAL to pages              │
└───────────────────────────────────────┘
```

## Features

- **Quorum Consensus**: Raft-like consensus protocol for leader election
- **WAL Replication**: Replicates WAL across multiple Safekeeper replicas
- **Durability**: WAL is durably stored before acknowledging to compute nodes
- **High Availability**: Multiple replicas with automatic leader election
- **Authentication**: API key and token-based authentication
- **TLS/HTTPS**: Encrypted communication

## Building

```bash
cd page-server
./build.sh
```

This builds both Page Server and Safekeeper.

## Running

### Single Safekeeper (Development)

```bash
./safekeeper -port 8090 -replica-id safekeeper-1
```

### Multiple Safekeepers (Production)

```bash
# Safekeeper 1 (Leader)
./safekeeper -port 8090 \
  -replica-id safekeeper-1 \
  -peers "http://localhost:8091,http://localhost:8092" \
  -api-key "your-secret-key"

# Safekeeper 2 (Follower)
./safekeeper -port 8091 \
  -replica-id safekeeper-2 \
  -peers "http://localhost:8090,http://localhost:8092" \
  -api-key "your-secret-key"

# Safekeeper 3 (Follower)
./safekeeper -port 8092 \
  -replica-id safekeeper-3 \
  -peers "http://localhost:8090,http://localhost:8091" \
  -api-key "your-secret-key"
```

### With TLS

```bash
./safekeeper -port 8090 \
  -replica-id safekeeper-1 \
  -tls -tls-cert ./certs/server.crt -tls-key ./certs/server.key \
  -api-key "your-secret-key"
```

## Command-Line Options

- `-port`: Server port (default: 8090)
- `-data-dir`: Data directory for WAL storage (default: ./safekeeper-data)
- `-replica-id`: Unique identifier for this Safekeeper replica
- `-peers`: Comma-separated list of peer Safekeeper endpoints
- `-api-key`: API key for authentication (optional)
- `-auth-tokens`: Comma-separated list of auth tokens (optional)
- `-tls`: Enable TLS/HTTPS (default: false)
- `-tls-cert`: Path to TLS certificate file (required if TLS enabled)
- `-tls-key`: Path to TLS private key file (required if TLS enabled)

## API Endpoints

### Public Endpoints

- `GET /api/v1/ping` - Health check
- `GET /api/v1/metrics` - Safekeeper metrics
- `GET /api/v1/get_wal?lsn=<lsn>` - Retrieve WAL record by LSN
- `GET /api/v1/get_latest_lsn` - Get latest LSN stored

### Protected Endpoints (Require Authentication)

- `POST /api/v1/stream_wal` - Stream WAL record from compute node

### Internal Endpoints (Replication/Consensus)

- `POST /api/v1/replicate_wal` - Replicate WAL from peer Safekeeper
- `POST /api/v1/request_vote` - Request vote during election
- `POST /api/v1/heartbeat` - Receive heartbeat from leader

## Consensus Protocol

Safekeeper uses a Raft-like consensus protocol:

1. **Leader Election**: When no heartbeat is received, followers start an election
2. **Quorum Voting**: Majority of replicas must vote for a leader
3. **WAL Replication**: Leader replicates WAL to all followers
4. **Quorum Acknowledgment**: WAL is acknowledged only after quorum is reached

### Quorum Size

Quorum size is calculated as: `(number_of_replicas / 2) + 1`

- 3 replicas → quorum = 2
- 5 replicas → quorum = 3
- 7 replicas → quorum = 4

## WAL Storage Format

WAL records are stored on disk as:

```
wal_<lsn>
├── LSN (8 bytes, little-endian)
├── WAL Data Length (4 bytes, little-endian)
└── WAL Data (variable length)
```

## Metrics

The `/api/v1/metrics` endpoint returns:

```json
{
  "status": "success",
  "metrics": {
    "replica_id": "safekeeper-1",
    "state": "leader",
    "term": 1,
    "latest_lsn": 12345,
    "wal_count": 1000,
    "quorum_size": 2,
    "peer_count": 2,
    "replication_lag": "0s"
  }
}
```

## Integration with Page Server

The Page Server should pull WAL from Safekeeper instead of receiving it directly from compute nodes. This ensures:

1. **Durability**: WAL is durably stored before processing
2. **Consistency**: Quorum ensures WAL is replicated
3. **Recovery**: Page Server can replay WAL from Safekeeper

## Status

✅ **Implemented:**
- Safekeeper service structure
- Raft-like consensus protocol
- WAL storage and retrieval
- Quorum-based replication
- Authentication and TLS
- Metrics and monitoring

⚠️ **Partially Implemented:**
- Peer replication (HTTP calls to peers)
- Leader election (vote requests)
- Heartbeat mechanism

❌ **Not Yet Implemented:**
- Full peer-to-peer communication
- Automatic failover
- WAL compression
- S3 backup for WAL

## Next Steps

1. Implement HTTP client for peer communication
2. Complete leader election with vote requests
3. Implement heartbeat mechanism
4. Update Page Server to pull WAL from Safekeeper
5. Update MariaDB integration to stream to Safekeeper

