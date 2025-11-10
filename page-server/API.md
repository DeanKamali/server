# Page Server HTTP API Documentation

## Overview

The Page Server provides a simple HTTP/JSON API for remote page storage and WAL streaming.

## Base URL

```
http://<host>:<port>/api/v1
```

Default port: `8080`

## Endpoints

### 1. Get Page

Fetch a single page from the Page Server.

**Endpoint:** `POST /api/v1/get_page`

**Request:**
```json
{
  "space_id": 1,
  "page_no": 42,
  "lsn": 1000
}
```

**Response (Success):**
```json
{
  "status": "success",
  "page_data": "base64_encoded_page_data",
  "page_lsn": 1000
}
```

**Response (Error):**
```json
{
  "status": "error",
  "error": "Page not found: space=1 page=42"
}
```

**Example with curl:**
```bash
curl -X POST http://localhost:8080/api/v1/get_page \
  -H "Content-Type: application/json" \
  -d '{"space_id":1,"page_no":42,"lsn":1000}'
```

---

### 2. Get Pages (Batch)

Fetch multiple pages in a single request. This is optimized for performance with parallel processing.

**Endpoint:** `POST /api/v1/get_pages`

**Request:**
```json
{
  "pages": [
    {"space_id": 1, "page_no": 42, "lsn": 1000},
    {"space_id": 1, "page_no": 43, "lsn": 1000},
    {"space_id": 1, "page_no": 44, "lsn": 1000}
  ]
}
```

**Response (Success):**
```json
{
  "status": "success",
  "pages": [
    {
      "space_id": 1,
      "page_no": 42,
      "status": "success",
      "page_data": "base64_encoded_page_data",
      "page_lsn": 1000
    },
    {
      "space_id": 1,
      "page_no": 43,
      "status": "success",
      "page_data": "base64_encoded_page_data",
      "page_lsn": 1000
    },
    {
      "space_id": 1,
      "page_no": 44,
      "status": "success",
      "page_data": "base64_encoded_page_data",
      "page_lsn": 1000
    }
  ]
}
```

**Response (Partial Success):**
```json
{
  "status": "partial",
  "pages": [
    {
      "space_id": 1,
      "page_no": 42,
      "status": "success",
      "page_data": "base64_encoded_page_data",
      "page_lsn": 1000
    },
    {
      "space_id": 1,
      "page_no": 43,
      "status": "error",
      "error": "Page not found: space=1 page=43 lsn=1000"
    }
  ]
}
```

**Example with curl:**
```bash
curl -X POST http://localhost:8080/api/v1/get_pages \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-secret-key" \
  -d '{
    "pages": [
      {"space_id": 1, "page_no": 42, "lsn": 1000},
      {"space_id": 1, "page_no": 43, "lsn": 1000}
    ]
  }'
```

**Features:**
- **Parallel Processing**: All pages are fetched concurrently using goroutines
- **Efficient**: Single HTTP request for multiple pages
- **Partial Success**: Returns status "partial" if some pages fail
- **Max Pages**: Limited to 1000 pages per request
- **Cache Aware**: Uses cache when available, falls back to storage

**Performance Benefits:**
- Reduces HTTP overhead (1 request vs N requests)
- Parallel I/O operations
- Better network utilization
- Lower latency for multiple pages

---

### 3. Stream WAL

Stream a WAL (redo log) record to the Page Server.

**Endpoint:** `POST /api/v1/stream_wal`

**Request:**
```json
{
  "lsn": 1000,
  "wal_data": "base64_encoded_wal_data",
  "space_id": 1,
  "page_no": 42
}
```

**Response:**
```json
{
  "status": "success",
  "last_applied_lsn": 1000
}
```

**Example with curl:**
```bash
curl -X POST http://localhost:8080/api/v1/stream_wal \
  -H "Content-Type: application/json" \
  -d '{"lsn":1000,"wal_data":"SGVsbG8gV29ybGQ=","space_id":1,"page_no":42}'
```

---

### 3. Ping (Health Check)

Check if the Page Server is running.

**Endpoint:** `GET /api/v1/ping`

**Response:**
```json
{
  "status": "ok",
  "version": "1.0.0"
}
```

**Example with curl:**
```bash
curl http://localhost:8080/api/v1/ping
```

---

## Data Encoding

- **Page Data**: Binary page data is base64-encoded in JSON responses
- **WAL Data**: WAL records are base64-encoded in JSON requests
- **LSN**: Log Sequence Number (uint64)

## Error Handling

All endpoints return HTTP status codes:
- `200 OK` - Success
- `400 Bad Request` - Invalid request format
- `404 Not Found` - Page not found (for GetPage)
- `405 Method Not Allowed` - Wrong HTTP method
- `500 Internal Server Error` - Server error

Error responses include a JSON body with `status: "error"` and an `error` field describing the issue.

### 5. Time-Travel Queries

Query pages at a specific point in time (LSN). This enables point-in-time recovery and historical data access.

**Endpoint:** `POST /api/v1/time_travel`

**Request:**
```json
{
  "space_id": 1,
  "page_no": 42,
  "lsn": 5000
}
```

**Response (Success):**
```json
{
  "status": "success",
  "page_data": "base64_encoded_page_data",
  "page_lsn": 5000
}
```

**Response (Error):**
```json
{
  "status": "error",
  "error": "Page not found at LSN 5000: space=1 page=42"
}
```

**Example:**
```bash
curl -X POST http://localhost:8080/api/v1/time_travel \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-secret-key" \
  -d '{"space_id":1,"page_no":42,"lsn":5000}'
```

**Use Cases:**
- Point-in-time recovery
- Historical data analysis
- Audit queries
- Data forensics

---

### 6. Snapshots

Create and manage point-in-time snapshots of the database state.

#### 6.1 Create Snapshot

**Endpoint:** `POST /api/v1/snapshots/create`

**Request:**
```json
{
  "lsn": 10000,
  "description": "Before major migration"
}
```

If `lsn` is 0 or omitted, uses the latest LSN.

**Response:**
```json
{
  "status": "success",
  "snapshot": {
    "id": "snapshot_10000_1699123456",
    "lsn": 10000,
    "timestamp": "2025-11-09T16:30:00Z",
    "description": "Before major migration"
  }
}
```

#### 6.2 List Snapshots

**Endpoint:** `GET /api/v1/snapshots/list`

**Response:**
```json
{
  "status": "success",
  "snapshots": [
    {
      "id": "snapshot_10000_1699123456",
      "lsn": 10000,
      "timestamp": "2025-11-09T16:30:00Z",
      "description": "Before major migration"
    }
  ]
}
```

#### 6.3 Get Snapshot

**Endpoint:** `GET /api/v1/snapshots/get?id=<snapshot_id>`

**Response:**
```json
{
  "status": "success",
  "snapshot": {
    "id": "snapshot_10000_1699123456",
    "lsn": 10000,
    "timestamp": "2025-11-09T16:30:00Z",
    "description": "Before major migration"
  }
}
```

#### 6.4 Restore Snapshot

**Endpoint:** `POST /api/v1/snapshots/restore`

**Request:**
```json
{
  "snapshot_id": "snapshot_10000_1699123456"
}
```

**Response:**
```json
{
  "status": "success",
  "message": "Snapshot restored. Use time-travel queries with LSN to access pages at this point in time.",
  "snapshot": {
    "id": "snapshot_10000_1699123456",
    "lsn": 10000,
    "timestamp": "2025-11-09T16:30:00Z"
  },
  "usage": {
    "lsn": 10000,
    "note": "Query pages using get_page or get_pages with lsn=10000"
  }
}
```

**Note**: Restoring a snapshot doesn't modify current data. Instead, it provides the LSN to use with time-travel queries to access pages at that point in time.

**Example Workflow:**
```bash
# 1. Create snapshot
curl -X POST http://localhost:8080/api/v1/snapshots/create \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{"description":"Before migration"}'

# 2. List snapshots
curl -H "X-API-Key: your-key" \
  http://localhost:8080/api/v1/snapshots/list

# 3. Restore snapshot (get LSN)
curl -X POST http://localhost:8080/api/v1/snapshots/restore \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{"snapshot_id":"snapshot_10000_1699123456"}'

# 4. Query pages at snapshot LSN
curl -X POST http://localhost:8080/api/v1/time_travel \
  -H "X-API-Key: your-key" \
  -H "Content-Type: application/json" \
  -d '{"space_id":1,"page_no":42,"lsn":10000}'
```

---

### 7. Metrics

Get Page Server metrics and statistics.

**Endpoint:** `GET /api/v1/metrics`

**Response:**
```json
{
  "cache": {
    "size": 42,
    "max_size": 1000,
    "evict_count": 5
  },
  "storage": {
    "latest_lsn": 123456
  }
}
```

**Example with curl:**
```bash
curl http://localhost:8080/api/v1/metrics
```

---

## Authentication

All endpoints except `/api/v1/ping` require authentication if enabled. The server supports:

1. **API Key** (Header: `X-API-Key`)
2. **Bearer Token** (Header: `Authorization: Bearer <token>`)
3. **Basic Auth** (Header: `Authorization: Basic <base64>`)

**Example with API Key:**
```bash
curl -H "X-API-Key: your-secret-key" \
  -X POST http://localhost:8080/api/v1/get_page \
  -H "Content-Type: application/json" \
  -d '{"space_id":1,"page_no":42,"lsn":1000}'
```

**Example with Bearer Token:**
```bash
curl -H "Authorization: Bearer your-token" \
  -X POST http://localhost:8080/api/v1/get_page \
  -H "Content-Type: application/json" \
  -d '{"space_id":1,"page_no":42,"lsn":1000}'
```

## TLS/HTTPS

The server supports TLS/HTTPS for encrypted communication. Use the `-tls`, `-tls-cert`, and `-tls-key` flags to enable.

**Example:**
```bash
curl -k https://localhost:8443/api/v1/ping
```

## Current Features

✅ **Persistent Storage**: File-based storage with page versioning
✅ **WAL Application**: WAL records are applied to pages
✅ **Page Versioning**: Pages stored with LSN-based versioning
✅ **LRU Cache**: In-memory cache with eviction policy
✅ **Metrics**: Monitoring endpoint for cache and storage stats
✅ **Authentication**: API key and Bearer token support
✅ **TLS/HTTPS**: Full TLS 1.2+ support

## Current Limitations

- ⚠️ WAL parsing is simplified (not full InnoDB redo log parsing)
- ⚠️ File-based storage (not object storage like S3)

## Future Enhancements

- Full InnoDB redo log parsing
- Object storage backend (S3/MinIO)
- High availability (multiple replicas)
- Snapshot support
- Time-travel queries


