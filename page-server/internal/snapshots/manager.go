package snapshots

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/linux/projects/server/page-server/pkg/types"
)

// SnapshotManager manages database snapshots
type SnapshotManager struct {
	snapshotsDir string
	snapshots    map[string]*types.Snapshot
	mu           sync.RWMutex
}

// NewSnapshotManager creates a new snapshot manager
func NewSnapshotManager(baseDir string) (*SnapshotManager, error) {
	snapshotsDir := filepath.Join(baseDir, "snapshots")
	if err := os.MkdirAll(snapshotsDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create snapshots directory: %w", err)
	}

	sm := &SnapshotManager{
		snapshotsDir: snapshotsDir,
		snapshots:    make(map[string]*types.Snapshot),
	}

	// Load existing snapshots
	if err := sm.loadSnapshots(); err != nil {
		return nil, fmt.Errorf("failed to load snapshots: %w", err)
	}

	return sm, nil
}

// CreateSnapshot creates a new snapshot at the current LSN
func (sm *SnapshotManager) CreateSnapshot(lsn uint64, description string) (*types.Snapshot, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	snapshotID := fmt.Sprintf("snapshot_%d_%d", lsn, time.Now().Unix())
	snapshot := &types.Snapshot{
		ID:          snapshotID,
		LSN:         lsn,
		Timestamp:   time.Now(),
		Description: description,
	}

	// Save snapshot metadata
	snapshotFile := filepath.Join(sm.snapshotsDir, fmt.Sprintf("%s.json", snapshotID))
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	if err := os.WriteFile(snapshotFile, data, 0644); err != nil {
		return nil, fmt.Errorf("failed to save snapshot: %w", err)
	}

	sm.snapshots[snapshotID] = snapshot
	return snapshot, nil
}

// GetSnapshot retrieves a snapshot by ID
func (sm *SnapshotManager) GetSnapshot(id string) (*types.Snapshot, error) {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	snapshot, exists := sm.snapshots[id]
	if !exists {
		return nil, fmt.Errorf("snapshot not found: %s", id)
	}

	// Return a copy to prevent modification
	snapshotCopy := *snapshot
	return &snapshotCopy, nil
}

// ListSnapshots returns all snapshots
func (sm *SnapshotManager) ListSnapshots() []*types.Snapshot {
	sm.mu.RLock()
	defer sm.mu.RUnlock()

	snapshots := make([]*types.Snapshot, 0, len(sm.snapshots))
	for _, snapshot := range sm.snapshots {
		snapshots = append(snapshots, snapshot)
	}

	return snapshots
}

// DeleteSnapshot deletes a snapshot
func (sm *SnapshotManager) DeleteSnapshot(id string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	_, exists := sm.snapshots[id]
	if !exists {
		return fmt.Errorf("snapshot not found: %s", id)
	}

	// Delete snapshot file
	snapshotFile := filepath.Join(sm.snapshotsDir, fmt.Sprintf("%s.json", id))
	if err := os.Remove(snapshotFile); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete snapshot file: %w", err)
	}

	delete(sm.snapshots, id)
	return nil
}

// loadSnapshots loads all snapshots from disk
func (sm *SnapshotManager) loadSnapshots() error {
	entries, err := os.ReadDir(sm.snapshotsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No snapshots yet
		}
		return err
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}

		snapshotFile := filepath.Join(sm.snapshotsDir, entry.Name())
		data, err := os.ReadFile(snapshotFile)
		if err != nil {
			continue // Skip corrupted snapshots
		}

		var snapshot types.Snapshot
		if err := json.Unmarshal(data, &snapshot); err != nil {
			continue // Skip invalid snapshots
		}

		sm.snapshots[snapshot.ID] = &snapshot
	}

	return nil
}
