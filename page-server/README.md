# Page Server Implementation

This is a minimal Page Server implementation for the Neon-style InnoDB architecture.

## Overview

The Page Server is a remote service that:
- Stores page images at different LSN versions
- Receives and applies WAL (redo log) records
- Serves pages to stateless MySQL compute nodes on demand

## Architecture

```
MySQL Compute Node
     |
     | RPC: GetPage(space_id, page_no, lsn)
     ↓
Page Server (this service)
     |
     | Background: Apply WAL, merge pages
     ↓
Object Storage (S3/MinIO - future)
```

## Building

```bash
cd page-server
chmod +x build.sh
./build.sh
```

Or manually:
```bash
go build -o page-server main.go
```

## Running

```bash
# Basic usage
./page-server -port 8080

# With custom data directory and cache size
./page-server -port 8080 -data-dir /var/lib/page-server -cache-size 5000

# With authentication
./page-server -port 8080 -api-key "your-secret-api-key"

# With TLS/HTTPS
./page-server -port 8443 -tls -tls-cert ./certs/server.crt -tls-key ./certs/server.key

# With both authentication and TLS
./page-server -port 8443 \
  -api-key "your-secret-api-key" \
  -tls -tls-cert ./certs/server.crt -tls-key ./certs/server.key

# With S3/Object Storage (Wasabi example)
./page-server -port 8080 \
  -storage-backend s3 \
  -s3-endpoint https://s3.wasabisys.com \
  -s3-bucket sb-mariadb \
  -s3-region us-east-1 \
  -s3-access-key YOUR_ACCESS_KEY \
  -s3-secret-key YOUR_SECRET_KEY \
  -s3-use-ssl true \
  -api-key "your-secret-api-key"

# With MinIO (local testing)
./page-server -port 8080 \
  -storage-backend s3 \
  -s3-endpoint http://localhost:9000 \
  -s3-bucket page-server-data \
  -s3-access-key minioadmin \
  -s3-secret-key minioadmin \
  -s3-use-ssl false

# With Hybrid Storage (Neon-style tiered caching: Memory + Disk + S3)
./page-server -port 8080 \
  -storage-backend hybrid \
  -data-dir ./page-server-data \
  -cache-size 1000 \
  -s3-endpoint https://s3.wasabisys.com \
  -s3-bucket sb-mariadb \
  -s3-region us-east-1 \
  -s3-access-key YOUR_ACCESS_KEY \
  -s3-secret-key YOUR_SECRET_KEY \
  -s3-use-ssl true \
  -api-key "your-secret-key"
```

**Command-line options:**
- `-port`: Server port (default: 8080)
- `-data-dir`: Data directory for persistent storage (default: ./page-server-data)
- `-cache-size`: Maximum number of pages in cache (default: 1000)
- `-api-key`: API key for authentication (optional)
- `-auth-tokens`: Comma-separated list of auth tokens (optional)
- `-tls`: Enable TLS/HTTPS (default: false)
- `-tls-cert`: Path to TLS certificate file (required if TLS enabled)
- `-tls-key`: Path to TLS private key file (required if TLS enabled)

**Storage Backend options:**
- `-storage-backend`: Storage backend type: `file`, `s3`, or `hybrid` (default: `file`)
  - `file`: Local filesystem only
  - `s3`: S3-compatible object storage only
  - `hybrid`: Neon-style tiered caching (Memory → Disk → S3)

**S3/Object Storage options:**
- `-s3-endpoint`: S3 endpoint URL (e.g., `https://s3.amazonaws.com` or `http://minio:9000`)
- `-s3-bucket`: S3 bucket name (required for S3 backend)
- `-s3-region`: AWS region (default: `us-east-1`)
- `-s3-access-key`: S3 access key ID (optional, uses IAM role if not provided)
- `-s3-secret-key`: S3 secret access key (optional, uses IAM role if not provided)
- `-s3-prefix`: Optional prefix for S3 objects (default: empty)
- `-s3-use-ssl`: Use SSL/TLS for S3 connections (default: `true`)

## Protocol

The Page Server uses **HTTP/JSON** for simplicity and fast iteration. See `API.md` for complete API documentation.

**Endpoints:**
- `POST /api/v1/get_page` - Fetch a single page (with LSN versioning)
- `POST /api/v1/get_pages` - Fetch multiple pages in batch (parallel processing)
- `POST /api/v1/stream_wal` - Stream WAL record (applied to pages)
- `GET /api/v1/ping` - Health check
- `GET /api/v1/metrics` - Metrics and statistics

## Current Implementation Status

**✅ Implemented Features:**
- HTTP/JSON server
- GetPage endpoint with LSN-based versioning
- **GetPages batch endpoint** with parallel processing
- StreamWAL endpoint with WAL application
- Ping health check
- Metrics endpoint
- **Persistent file-based storage** with page versioning
- **WAL application** to pages (simplified)
- **LRU page cache** with eviction policy
- Base64 encoding for binary data

**✅ Fully Implemented:**
- **Full InnoDB redo log parsing** - Complete parser for MariaDB 10.8+ physical redo log format
- **Time-travel queries** - Query pages at any point in time (LSN-based)
- **Snapshots** - Create and restore point-in-time snapshots

**✅ Security Features:**
- **Authentication**: API key and Bearer token support
- **TLS/HTTPS**: Full TLS 1.2+ support with configurable certificates

**⚠️ Partially Implemented:**
- Extended record subtypes (parsed but not all subtypes implemented)

**✅ Fully Implemented:**
- **S3/Object Storage Backend** - Complete S3-compatible storage (AWS S3, Wasabi, MinIO)
- **Hybrid Storage (Neon-Style Tiered Caching)** - Three-tier storage: Memory → Disk → S3

**❌ Not Yet Implemented:**
- High availability (multiple replicas)

## Storage Layout

The Page Server stores data in the following directory structure:

```
page-server-data/
├── pages/
│   └── space_<id>/
│       ├── page_<no>_<lsn>      # Versioned page files
│       └── page_<no>_latest     # Symlink to latest version
└── wal/
    └── wal_<lsn>                # WAL record files
```

## Authentication

The Page Server supports multiple authentication methods:

### API Key
```bash
# Start server with API key
./page-server -api-key "your-secret-key"

# Use in requests
curl -H "X-API-Key: your-secret-key" \
  -X POST http://localhost:8080/api/v1/get_page \
  -H "Content-Type: application/json" \
  -d '{"space_id":1,"page_no":42,"lsn":1000}'
```

### Bearer Token
```bash
# Start server with auth tokens
./page-server -auth-tokens "token1,token2,token3"

# Use in requests
curl -H "Authorization: Bearer token1" \
  -X POST http://localhost:8080/api/v1/get_page \
  -H "Content-Type: application/json" \
  -d '{"space_id":1,"page_no":42,"lsn":1000}'
```

### Basic Auth
```bash
# Use API key or token as password
curl -u "user:your-secret-key" \
  -X POST http://localhost:8080/api/v1/get_page \
  -H "Content-Type: application/json" \
  -d '{"space_id":1,"page_no":42,"lsn":1000}'
```

**Note**: The `/api/v1/ping` endpoint does not require authentication.

## TLS/HTTPS

### Generate Self-Signed Certificate (for testing)
```bash
./generate-cert.sh ./certs
```

### Start Server with TLS
```bash
./page-server -port 8443 \
  -tls \
  -tls-cert ./certs/server.crt \
  -tls-key ./certs/server.key
```

### Test with curl
```bash
# Skip certificate verification (for self-signed certs)
curl -k https://localhost:8443/api/v1/ping
```

**⚠️ Warning**: Self-signed certificates are for testing only. For production, use certificates from a proper Certificate Authority (CA).

## Next Steps

1. ✅ ~~Implement persistent page storage~~ (Done - file-based)
2. ✅ ~~Implement WAL application logic~~ (Done - simplified)
3. ✅ ~~Add page cache/eviction~~ (Done - LRU cache)
4. ✅ ~~Add monitoring/metrics~~ (Done - metrics endpoint)
5. ✅ ~~Add authentication/authorization~~ (Done - API key & tokens)
6. ✅ ~~Add TLS/HTTPS support~~ (Done - TLS 1.2+)
7. Implement full InnoDB redo log parsing
8. Add object storage backend (S3/MinIO)
9. Performance optimization

## Testing

```bash
# Start server
./page-server -port 8080

# In another terminal, test with curl
# Ping
curl http://localhost:8080/api/v1/ping

# Get page
curl -X POST http://localhost:8080/api/v1/get_page \
  -H "Content-Type: application/json" \
  -d '{"space_id":1,"page_no":42,"lsn":1000}'

# Stream WAL
curl -X POST http://localhost:8080/api/v1/stream_wal \
  -H "Content-Type: application/json" \
  -d '{"lsn":1000,"wal_data":"SGVsbG8gV29ybGQ="}'
```

See `API.md` for detailed API documentation.

