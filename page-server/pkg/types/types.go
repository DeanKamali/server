package types

import "time"

// Request/Response structures
type GetPageRequest struct {
	SpaceID uint32 `json:"space_id"`
	PageNo  uint32 `json:"page_no"`
	LSN     uint64 `json:"lsn"`
}

type GetPageResponse struct {
	Status   string `json:"status"`
	PageData string `json:"page_data,omitempty"` // Base64 encoded
	PageLSN  uint64 `json:"page_lsn,omitempty"`
	Error    string `json:"error,omitempty"`
}

type StreamWALRequest struct {
	LSN     uint64 `json:"lsn"`
	WALData string `json:"wal_data"` // Base64 encoded
	SpaceID uint32 `json:"space_id,omitempty"`
	PageNo  uint32 `json:"page_no,omitempty"`
}

type StreamWALResponse struct {
	Status         string `json:"status"`
	LastAppliedLSN uint64 `json:"last_applied_lsn,omitempty"`
	Error          string `json:"error,omitempty"`
}

type PingResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

// Batch request/response structures
type PageRequest struct {
	SpaceID uint32 `json:"space_id"`
	PageNo  uint32 `json:"page_no"`
	LSN     uint64 `json:"lsn"`
}

type GetPagesRequest struct {
	Pages []PageRequest `json:"pages"`
}

type PageResponse struct {
	SpaceID  uint32 `json:"space_id"`
	PageNo   uint32 `json:"page_no"`
	Status   string `json:"status"`
	PageData string `json:"page_data,omitempty"` // Base64 encoded
	PageLSN  uint64 `json:"page_lsn,omitempty"`
	Error    string `json:"error,omitempty"`
}

type GetPagesResponse struct {
	Pages  []PageResponse `json:"pages"`
	Status string         `json:"status"` // "success" or "partial" (some pages failed)
}

// Time-travel and snapshot request/response structures
type TimeTravelRequest struct {
	SpaceID uint32 `json:"space_id"`
	PageNo  uint32 `json:"page_no"`
	LSN     uint64 `json:"lsn"` // Point in time (LSN)
}

type CreateSnapshotRequest struct {
	LSN         uint64 `json:"lsn,omitempty"` // If 0, uses latest LSN
	Description string `json:"description,omitempty"`
}

type CreateSnapshotResponse struct {
	Status   string    `json:"status"`
	Snapshot *Snapshot `json:"snapshot,omitempty"`
	Error    string    `json:"error,omitempty"`
}

type ListSnapshotsResponse struct {
	Status    string      `json:"status"`
	Snapshots []*Snapshot `json:"snapshots"`
}

type RestoreSnapshotRequest struct {
	SnapshotID string `json:"snapshot_id"`
}

type Snapshot struct {
	ID          string    `json:"id"`
	LSN         uint64    `json:"lsn"`
	Timestamp   time.Time `json:"timestamp"`
	Description string    `json:"description,omitempty"`
}
