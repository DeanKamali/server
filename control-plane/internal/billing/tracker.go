package billing

import (
	"database/sql"
	"time"

	"github.com/linux/projects/server/control-plane/internal/state"
)

// UsageTracker tracks compute usage and billing metrics
// Mimics Neon's consumption metrics collection
type UsageTracker struct {
	stateStore state.StoreInterface
}

// NewUsageTracker creates a new usage tracker
func NewUsageTracker(stateStore state.StoreInterface) *UsageTracker {
	return &UsageTracker{
		stateStore: stateStore,
	}
}

// ComputeUsage represents compute node usage
type ComputeUsage struct {
	ID        string
	ProjectID string
	ComputeID string
	StartTime time.Time
	EndTime   *time.Time
	Seconds   int64
	CreatedAt time.Time
}

// StorageUsage represents storage usage
type StorageUsage struct {
	ID          string
	ProjectID   string
	StorageType string // "pageserver" or "safekeeper"
	Bytes       int64
	RecordedAt  time.Time
}

// RecordComputeStart records when a compute node becomes active
// Called when compute node is created or resumed
func (ut *UsageTracker) RecordComputeStart(projectID, computeID string) error {
	// Check if we're using SQLite or PostgreSQL
	db := ut.stateStore.GetDB()

	// Try PostgreSQL syntax first, fallback to SQLite
	_, err := db.Exec(`
		INSERT INTO compute_usage (id, project_id, compute_id, start_time, created_at)
		VALUES (gen_random_uuid(), $1, $2, NOW(), NOW())
		ON CONFLICT (compute_id) WHERE end_time IS NULL
		DO NOTHING
	`, projectID, computeID)

	// If PostgreSQL syntax fails, try SQLite
	if err != nil {
		_, err = db.Exec(`
			INSERT OR IGNORE INTO compute_usage (id, project_id, compute_id, start_time, created_at)
			VALUES (lower(hex(randomblob(16))), ?, ?, datetime('now'), datetime('now'))
		`, projectID, computeID)
	}
	return err
}

// RecordComputeStop records when a compute node becomes inactive
// Called when compute node is suspended or terminated
func (ut *UsageTracker) RecordComputeStop(computeID string) error {
	db := ut.stateStore.GetDB()

	// Try PostgreSQL syntax first
	_, err := db.Exec(`
		UPDATE compute_usage
		SET end_time = NOW(),
		    seconds = EXTRACT(EPOCH FROM (NOW() - start_time))::BIGINT
		WHERE compute_id = $1 AND end_time IS NULL
	`, computeID)

	// If PostgreSQL syntax fails, try SQLite
	if err != nil {
		_, err = db.Exec(`
			UPDATE compute_usage
			SET end_time = datetime('now'),
			    seconds = CAST((julianday('now') - julianday(start_time)) * 86400 AS INTEGER)
			WHERE compute_id = ? AND end_time IS NULL
		`, computeID)
	}
	return err
}

// RecordStorageUsage records storage usage for a project
func (ut *UsageTracker) RecordStorageUsage(projectID, storageType string, bytes int64) error {
	_, err := ut.stateStore.GetDB().Exec(`
		INSERT INTO storage_usage (id, project_id, storage_type, bytes, recorded_at)
		VALUES (gen_random_uuid(), $1, $2, $3, NOW())
	`, projectID, storageType, bytes)
	return err
}

// GetComputeUsage returns compute usage for a project in a time range
func (ut *UsageTracker) GetComputeUsage(projectID string, start, end time.Time) ([]ComputeUsage, error) {
	rows, err := ut.stateStore.GetDB().Query(`
		SELECT id, project_id, compute_id, start_time, end_time, seconds, created_at
		FROM compute_usage
		WHERE project_id = $1 AND start_time >= $2 AND start_time <= $3
		ORDER BY start_time DESC
	`, projectID, start, end)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var usages []ComputeUsage
	for rows.Next() {
		var usage ComputeUsage
		var endTime sql.NullTime
		err := rows.Scan(&usage.ID, &usage.ProjectID, &usage.ComputeID, &usage.StartTime, &endTime, &usage.Seconds, &usage.CreatedAt)
		if err != nil {
			return nil, err
		}
		if endTime.Valid {
			usage.EndTime = &endTime.Time
		}
		usages = append(usages, usage)
	}
	return usages, rows.Err()
}

// GetTotalComputeSeconds returns total compute seconds for a project
func (ut *UsageTracker) GetTotalComputeSeconds(projectID string, start, end time.Time) (int64, error) {
	var total int64
	err := ut.stateStore.GetDB().QueryRow(`
		SELECT COALESCE(SUM(seconds), 0)
		FROM compute_usage
		WHERE project_id = $1 AND start_time >= $2 AND start_time <= $3
	`, projectID, start, end).Scan(&total)
	return total, err
}

// GetStorageUsage returns storage usage for a project
func (ut *UsageTracker) GetStorageUsage(projectID string) ([]StorageUsage, error) {
	rows, err := ut.stateStore.GetDB().Query(`
		SELECT id, project_id, storage_type, bytes, recorded_at
		FROM storage_usage
		WHERE project_id = $1
		ORDER BY recorded_at DESC
	`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var usages []StorageUsage
	for rows.Next() {
		var usage StorageUsage
		err := rows.Scan(&usage.ID, &usage.ProjectID, &usage.StorageType, &usage.Bytes, &usage.RecordedAt)
		if err != nil {
			return nil, err
		}
		usages = append(usages, usage)
	}
	return usages, rows.Err()
}
