package safekeeper

import (
	"fmt"
	"log"
	"sync"
)

// RecoveryManager handles recovery from peer Safekeepers
type RecoveryManager struct {
	safekeeper *Safekeeper
	peerClient *PeerClient
	mu         sync.Mutex
}

// NewRecoveryManager creates a new recovery manager
func NewRecoveryManager(sk *Safekeeper) *RecoveryManager {
	return &RecoveryManager{
		safekeeper: sk,
		peerClient: sk.peerClient,
	}
}

// RecoveryState represents the state needed for recovery
type RecoveryState struct {
	LatestLSN    uint64
	Timelines    []*Timeline
	WALCount     uint64
	ReplicaID    string
	Term         uint64
}

// RecoverFromPeer recovers complete state from a peer Safekeeper
func (rm *RecoveryManager) RecoverFromPeer(peerEndpoint string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	log.Printf("Starting recovery from peer: %s", peerEndpoint)

	// Step 1: Get recovery state from peer
	state, err := rm.getRecoveryState(peerEndpoint)
	if err != nil {
		return fmt.Errorf("failed to get recovery state: %w", err)
	}

	log.Printf("Recovery state from peer: LSN=%d, WALCount=%d, Timelines=%d",
		state.LatestLSN, state.WALCount, len(state.Timelines))

	// Step 2: Sync timelines
	if err := rm.syncTimelines(peerEndpoint, state.Timelines); err != nil {
		return fmt.Errorf("failed to sync timelines: %w", err)
	}

	// Step 3: Sync WAL records
	if err := rm.syncWAL(peerEndpoint, state.LatestLSN); err != nil {
		return fmt.Errorf("failed to sync WAL: %w", err)
	}

	// Step 4: Update local state
	rm.safekeeper.lsnMu.Lock()
	rm.safekeeper.latestLSN = state.LatestLSN
	rm.safekeeper.lsnMu.Unlock()

	rm.safekeeper.stateMu.Lock()
	if state.Term > rm.safekeeper.term {
		rm.safekeeper.term = state.Term
	}
	rm.safekeeper.stateMu.Unlock()

	log.Printf("Recovery completed successfully from peer: %s", peerEndpoint)
	return nil
}

// getRecoveryState retrieves recovery state from a peer
func (rm *RecoveryManager) getRecoveryState(peerEndpoint string) (*RecoveryState, error) {
	// Get latest LSN
	latestLSN, err := rm.peerClient.GetLatestLSN(peerEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get latest LSN: %w", err)
	}

	// Get timelines
	timelines, err := rm.peerClient.GetTimelines(peerEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get timelines: %w", err)
	}

	// Get metrics for WAL count
	metrics, err := rm.peerClient.GetMetrics(peerEndpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get metrics: %w", err)
	}

	walCount := uint64(0)
	if count, ok := metrics["wal_count"].(float64); ok {
		walCount = uint64(count)
	}

	term := uint64(1)
	if t, ok := metrics["term"].(float64); ok {
		term = uint64(t)
	}

	replicaID := ""
	if id, ok := metrics["replica_id"].(string); ok {
		replicaID = id
	}

	return &RecoveryState{
		LatestLSN: latestLSN,
		Timelines: timelines,
		WALCount:  walCount,
		ReplicaID: replicaID,
		Term:      term,
	}, nil
}

// syncTimelines syncs timelines from peer
func (rm *RecoveryManager) syncTimelines(peerEndpoint string, peerTimelines []*Timeline) error {
	log.Printf("Syncing %d timelines from peer", len(peerTimelines))

	for _, peerTimeline := range peerTimelines {
		// Check if timeline exists locally
		_, err := rm.safekeeper.timelineManager.GetTimeline(peerTimeline.ID)
		if err != nil {
			// Timeline doesn't exist, create it
			timeline, err := rm.safekeeper.timelineManager.CreateTimeline(
				peerTimeline.ID,
				peerTimeline.ParentLSN,
				peerTimeline.ParentTimelineID,
			)
			if err != nil {
				log.Printf("Warning: Failed to create timeline %s: %v", peerTimeline.ID, err)
				continue
			}
			// Update LSN
			if err := rm.safekeeper.timelineManager.UpdateTimelineLSN(timeline.ID, peerTimeline.LatestLSN); err != nil {
				log.Printf("Warning: Failed to update timeline LSN: %v", err)
			}
		} else {
			// Timeline exists, update LSN if peer has newer data
			if err := rm.safekeeper.timelineManager.UpdateTimelineLSN(peerTimeline.ID, peerTimeline.LatestLSN); err != nil {
				log.Printf("Warning: Failed to update timeline LSN: %v", err)
			}
		}
	}

	log.Printf("Timeline sync completed")
	return nil
}

// syncWAL syncs WAL records from peer
func (rm *RecoveryManager) syncWAL(peerEndpoint string, targetLSN uint64) error {
	rm.safekeeper.lsnMu.RLock()
	localLSN := rm.safekeeper.latestLSN
	rm.safekeeper.lsnMu.RUnlock()

	if localLSN >= targetLSN {
		log.Printf("Local LSN (%d) is already up to date (target: %d)", localLSN, targetLSN)
		return nil
	}

	log.Printf("Syncing WAL from LSN %d to %d from peer", localLSN+1, targetLSN)

	// Sync WAL in batches
	batchSize := uint64(100)
	for lsn := localLSN + 1; lsn <= targetLSN; lsn += batchSize {
		endLSN := lsn + batchSize - 1
		if endLSN > targetLSN {
			endLSN = targetLSN
		}

		// Get WAL records in batch
		walRecords, err := rm.peerClient.GetWALRange(peerEndpoint, lsn, endLSN)
		if err != nil {
			return fmt.Errorf("failed to get WAL range %d-%d: %w", lsn, endLSN, err)
		}

		// Store WAL records locally
		for _, record := range walRecords {
			// Determine if WAL is compressed (assume same as our compression setting)
			isCompressed := rm.safekeeper.compressionEnabled
			if err := rm.safekeeper.storeWALLocal(record.LSN, record.WALData, isCompressed); err != nil {
				log.Printf("Warning: Failed to store WAL LSN %d: %v", record.LSN, err)
				continue
			}
		}

		log.Printf("Synced WAL batch: LSN %d-%d (%d records)", lsn, endLSN, len(walRecords))
	}

	log.Printf("WAL sync completed: %d to %d", localLSN+1, targetLSN)
	return nil
}

// RecoverTimeline recovers a specific timeline from peers
func (rm *RecoveryManager) RecoverTimeline(timelineID string, peerEndpoints []string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	log.Printf("Recovering timeline %s from peers", timelineID)

	// Try to recover from each peer until successful
	for _, peerEndpoint := range peerEndpoints {
		// Get timeline state from peer
		timeline, err := rm.peerClient.GetTimeline(peerEndpoint, timelineID)
		if err != nil {
			log.Printf("Failed to get timeline from %s: %v", peerEndpoint, err)
			continue
		}

		// Create timeline locally if it doesn't exist
		_, err = rm.safekeeper.timelineManager.GetTimeline(timelineID)
		if err != nil {
			// Timeline doesn't exist, create it
			_, err = rm.safekeeper.timelineManager.CreateTimeline(
				timeline.ID,
				timeline.ParentLSN,
				timeline.ParentTimelineID,
			)
			if err != nil {
				return fmt.Errorf("failed to create timeline: %w", err)
			}
		}

		// Update timeline LSN
		if err := rm.safekeeper.timelineManager.UpdateTimelineLSN(timelineID, timeline.LatestLSN); err != nil {
			return fmt.Errorf("failed to update timeline LSN: %w", err)
		}

		// Sync WAL for this timeline
		rm.safekeeper.lsnMu.RLock()
		localLSN := rm.safekeeper.latestLSN
		rm.safekeeper.lsnMu.RUnlock()

		if timeline.LatestLSN > localLSN {
			if err := rm.syncWAL(peerEndpoint, timeline.LatestLSN); err != nil {
				log.Printf("Warning: Failed to sync WAL for timeline: %v", err)
			}
		}

		log.Printf("Timeline %s recovered successfully from %s", timelineID, peerEndpoint)
		return nil
	}

	return fmt.Errorf("failed to recover timeline %s from any peer", timelineID)
}



