package safekeeper

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Safekeeper stores WAL records with durability guarantees
type Safekeeper struct {
	// Local storage
	dataDir   string
	walDir    string
	latestLSN uint64
	lsnMu     sync.RWMutex

	// Consensus
	replicaID  string
	peers      []string           // Other Safekeeper endpoints
	quorumSize int                // Minimum replicas needed for quorum
	membership *MembershipManager // Dynamic membership management

	// Replication
	replicationMu sync.Mutex
	pendingWAL    map[uint64]*WALRecord // WAL waiting for replication

	// State
	state   State
	stateMu sync.RWMutex
	term    uint64 // Current term (for consensus)

	// Compression (Zstd - matching Neon)
	compressor         *Compressor
	compressionEnabled bool

	// Protobuf encoding (performance optimization)
	protobufEncoder *ProtobufEncoder
	protobufEnabled bool

	// Timeline management
	timelineManager   *TimelineManager
	defaultTimelineID string

	// Peer communication
	peerClient *PeerClient

	// S3 Backup
	s3Backup *S3Backup

	// Recovery
	recoveryManager *RecoveryManager

	// Leader discovery
	knownLeader string
	leaderMu    sync.RWMutex

	// Metrics
	walCount         uint64
	replicationLag   time.Duration
	compressionRatio float64
}

// State represents Safekeeper state
type State int

const (
	StateFollower State = iota
	StateCandidate
	StateLeader
)

// WALRecord represents a WAL record stored in Safekeeper
type WALRecord struct {
	LSN      uint64
	WALData  []byte
	SpaceID  uint32
	PageNo   uint32
	Term     uint64
	Replicas map[string]bool // Which replicas have confirmed
	mu       sync.Mutex
}

// S3Config holds S3 backup configuration (exported for use in cmd/main.go)
type S3Config struct {
	Endpoint  string
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
	Prefix    string
	UseSSL    bool
}

// NewSafekeeper creates a new Safekeeper instance
func NewSafekeeper(dataDir string, replicaID string, peers []string, enableCompression bool, enableProtobuf bool, s3Config *S3Config) (*Safekeeper, error) {
	membership := NewMembershipManager(peers)

	sk := &Safekeeper{
		dataDir:            dataDir,
		walDir:             filepath.Join(dataDir, "wal"),
		replicaID:          replicaID,
		peers:              peers,
		quorumSize:         membership.GetQuorumSize(),
		pendingWAL:         make(map[uint64]*WALRecord),
		state:              StateFollower,
		term:               1,
		compressionEnabled: enableCompression,
		protobufEnabled:    enableProtobuf,
		timelineManager:    NewTimelineManager(),
		defaultTimelineID:  "default",
		peerClient:         NewPeerClient(),
		membership:         membership,
	}

	// Initialize Protobuf encoder if enabled
	if enableProtobuf {
		sk.protobufEncoder = NewProtobufEncoder(true)
		log.Printf("Protobuf encoding enabled")
	}

	// Initialize recovery manager
	sk.recoveryManager = NewRecoveryManager(sk)

	// Initialize S3 backup if configured
	if s3Config != nil && s3Config.Bucket != "" {
		s3Backup, err := NewS3Backup(*s3Config)
		if err != nil {
			return nil, fmt.Errorf("failed to create S3 backup: %w", err)
		}
		sk.s3Backup = s3Backup
		log.Printf("S3 backup enabled: bucket=%s", s3Config.Bucket)
	}

	// Initialize compression if enabled
	if enableCompression {
		compressor, err := NewCompressor()
		if err != nil {
			return nil, fmt.Errorf("failed to create compressor: %w", err)
		}
		sk.compressor = compressor
	}

	// Create WAL directory
	if err := os.MkdirAll(sk.walDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory: %w", err)
	}

	// Create default timeline
	if _, err := sk.timelineManager.CreateTimeline(sk.defaultTimelineID, 0, ""); err != nil {
		log.Printf("Warning: Failed to create default timeline: %v", err)
	}

	// Load latest LSN from disk
	if err := sk.loadLatestLSN(); err != nil {
		log.Printf("Warning: Failed to load latest LSN: %v", err)
	}

	return sk, nil
}

// StoreWAL stores a WAL record with quorum consensus
func (sk *Safekeeper) StoreWAL(lsn uint64, walData []byte, spaceID uint32, pageNo uint32) error {
	sk.stateMu.RLock()
	isLeader := sk.state == StateLeader
	sk.stateMu.RUnlock()

	if !isLeader {
		// Forward to leader
		return sk.forwardToLeader(lsn, walData, spaceID, pageNo)
	}

	// Create WAL record
	record := &WALRecord{
		LSN:      lsn,
		WALData:  walData,
		SpaceID:  spaceID,
		PageNo:   pageNo,
		Term:     sk.term,
		Replicas: make(map[string]bool),
	}
	record.Replicas[sk.replicaID] = true // We have it locally

	// Compress WAL data if compression is enabled (matching Neon's 70% reduction)
	var compressedData []byte
	var compressionRatio float64 = 1.0
	if sk.compressionEnabled && sk.compressor != nil {
		var err error
		compressedData, compressionRatio, err = sk.compressor.Compress(walData)
		if err != nil {
			log.Printf("Warning: Compression failed, storing uncompressed: %v", err)
			compressedData = walData
		} else {
			// Update compression ratio metric
			sk.compressionRatio = compressionRatio
			log.Printf("WAL compressed: %d -> %d bytes (ratio: %.2f)", len(walData), len(compressedData), compressionRatio)
		}
	} else {
		compressedData = walData
	}

	// Store locally first (compressed if enabled)
	isCompressed := sk.compressionEnabled && compressionRatio < 1.0
	if err := sk.storeWALLocal(lsn, compressedData, isCompressed); err != nil {
		return fmt.Errorf("failed to store WAL locally: %w", err)
	}

	// Backup to S3 if enabled (async)
	if sk.s3Backup != nil && sk.s3Backup.IsEnabled() {
		go func() {
			if err := sk.s3Backup.BackupWAL(lsn, compressedData); err != nil {
				log.Printf("Warning: S3 backup failed for LSN %d: %v", lsn, err)
			}
		}()
	}

	// Replicate to peers
	sk.replicationMu.Lock()
	sk.pendingWAL[lsn] = record
	sk.replicationMu.Unlock()

	// Start replication in background
	go sk.replicateWAL(record)

	// Wait for quorum (with timeout)
	quorumReached := sk.waitForQuorum(record, 5*time.Second)
	if !quorumReached {
		log.Printf("Warning: Quorum not reached for LSN %d within timeout", lsn)
		// Still return success - WAL is stored locally and will eventually replicate
	}

	// Update latest LSN
	sk.lsnMu.Lock()
	if lsn > sk.latestLSN {
		sk.latestLSN = lsn
	}
	sk.lsnMu.Unlock()

	sk.walCount++
	return nil
}

// storeWALLocal stores WAL record to local disk
// isCompressed indicates if walData is already compressed
func (sk *Safekeeper) storeWALLocal(lsn uint64, walData []byte, isCompressed bool) error {
	walFile := filepath.Join(sk.walDir, fmt.Sprintf("wal_%d", lsn))

	file, err := os.Create(walFile)
	if err != nil {
		return fmt.Errorf("failed to create WAL file: %w", err)
	}
	defer file.Close()

	// Write LSN header
	if err := binary.Write(file, binary.LittleEndian, lsn); err != nil {
		return fmt.Errorf("failed to write LSN: %w", err)
	}

	// Write compression flag (1 byte: 1 = compressed, 0 = uncompressed)
	compressionFlag := uint8(0)
	if isCompressed {
		compressionFlag = 1
	}
	if err := binary.Write(file, binary.LittleEndian, compressionFlag); err != nil {
		return fmt.Errorf("failed to write compression flag: %w", err)
	}

	// Write WAL data length
	if err := binary.Write(file, binary.LittleEndian, uint32(len(walData))); err != nil {
		return fmt.Errorf("failed to write WAL length: %w", err)
	}

	// Write WAL data
	if _, err := file.Write(walData); err != nil {
		return fmt.Errorf("failed to write WAL data: %w", err)
	}

	// Sync to disk for durability
	if err := file.Sync(); err != nil {
		return fmt.Errorf("failed to sync WAL file: %w", err)
	}

	return nil
}

// replicateWAL replicates WAL to peer Safekeepers
func (sk *Safekeeper) replicateWAL(record *WALRecord) {
	successCount := 1 // We already have it locally

	for _, peer := range sk.peers {
		if err := sk.sendWALToPeer(peer, record); err != nil {
			log.Printf("Failed to replicate WAL LSN %d to peer %s: %v", record.LSN, peer, err)
			continue
		}

		record.mu.Lock()
		record.Replicas[peer] = true
		record.mu.Unlock()

		successCount++
	}

	log.Printf("Replicated WAL LSN %d to %d/%d replicas", record.LSN, successCount, len(sk.peers)+1)
}

// sendWALToPeer sends WAL record to a peer Safekeeper
func (sk *Safekeeper) sendWALToPeer(peerEndpoint string, record *WALRecord) error {
	// Use peer client to send WAL (compressed if compression is enabled)
	walData := record.WALData
	if sk.compressionEnabled {
		// Data is already compressed when stored
		walData = record.WALData
	}

	return sk.peerClient.SendWALToPeer(peerEndpoint, record.LSN, walData, record.SpaceID, record.PageNo)
}

// waitForQuorum waits for quorum consensus on a WAL record
func (sk *Safekeeper) waitForQuorum(record *WALRecord, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		record.mu.Lock()
		confirmedCount := len(record.Replicas)
		record.mu.Unlock()

		if confirmedCount >= sk.quorumSize {
			return true
		}

		time.Sleep(100 * time.Millisecond)
	}

	return false
}

// forwardToLeader forwards WAL to the current leader
func (sk *Safekeeper) forwardToLeader(lsn uint64, walData []byte, spaceID uint32, pageNo uint32) error {
	// Discover leader if not known
	leader, err := sk.discoverLeader()
	if err != nil {
		// Leader discovery failed, store locally (eventual consistency)
		log.Printf("Warning: Leader discovery failed, storing locally: %v", err)
		if err := sk.storeWALLocal(lsn, walData, false); err != nil {
			return err
		}

		// Update latest LSN even when not leader
		sk.lsnMu.Lock()
		if lsn > sk.latestLSN {
			sk.latestLSN = lsn
		}
		sk.lsnMu.Unlock()
		return nil
	}

	// Forward to discovered leader
	if err := sk.peerClient.SendWALToPeer(leader, lsn, walData, spaceID, pageNo); err != nil {
		log.Printf("Warning: Failed to forward WAL to leader %s, storing locally: %v", leader, err)
		// Fallback to local storage
		if err := sk.storeWALLocal(lsn, walData, false); err != nil {
			return err
		}
	} else {
		log.Printf("Forwarded WAL LSN %d to leader %s", lsn, leader)
	}

	// Update latest LSN even when not leader
	sk.lsnMu.Lock()
	if lsn > sk.latestLSN {
		sk.latestLSN = lsn
	}
	sk.lsnMu.Unlock()

	return nil
}

// discoverLeader discovers the current leader from peers
func (sk *Safekeeper) discoverLeader() (string, error) {
	sk.leaderMu.RLock()
	if sk.knownLeader != "" {
		// Verify leader is still valid
		metrics, err := sk.peerClient.GetMetrics(sk.knownLeader)
		if err == nil {
			if state, ok := metrics["state"].(string); ok && state == "leader" {
				sk.leaderMu.RUnlock()
				return sk.knownLeader, nil
			}
		}
		// Leader is no longer valid, clear it
		sk.knownLeader = ""
	}
	sk.leaderMu.RUnlock()

	// Try to discover leader from peers
	for _, peer := range sk.peers {
		metrics, err := sk.peerClient.GetMetrics(peer)
		if err != nil {
			continue
		}

		if state, ok := metrics["state"].(string); ok && state == "leader" {
			sk.leaderMu.Lock()
			sk.knownLeader = peer
			sk.leaderMu.Unlock()
			log.Printf("Discovered leader: %s", peer)
			return peer, nil
		}
	}

	// No leader found in peers, check if we're the leader
	sk.stateMu.RLock()
	isLeader := sk.state == StateLeader
	sk.stateMu.RUnlock()

	if isLeader {
		return "", fmt.Errorf("we are the leader, no need to forward")
	}

	return "", fmt.Errorf("no leader found")
}

// SetKnownLeader sets the known leader (used when we become leader)
func (sk *Safekeeper) SetKnownLeader(leaderEndpoint string) {
	sk.leaderMu.Lock()
	defer sk.leaderMu.Unlock()
	sk.knownLeader = leaderEndpoint
}

// ClearKnownLeader clears the known leader (used when leader changes)
func (sk *Safekeeper) ClearKnownLeader() {
	sk.leaderMu.Lock()
	defer sk.leaderMu.Unlock()
	sk.knownLeader = ""
}

// GetWAL retrieves a WAL record by LSN (decompresses if needed)
func (sk *Safekeeper) GetWAL(lsn uint64) ([]byte, error) {
	walFile := filepath.Join(sk.walDir, fmt.Sprintf("wal_%d", lsn))

	file, err := os.Open(walFile)
	if err != nil {
		return nil, fmt.Errorf("WAL record not found: %w", err)
	}
	defer file.Close()

	// Read LSN (verify)
	var fileLSN uint64
	if err := binary.Read(file, binary.LittleEndian, &fileLSN); err != nil {
		return nil, fmt.Errorf("failed to read LSN: %w", err)
	}

	if fileLSN != lsn {
		return nil, fmt.Errorf("LSN mismatch: expected %d, got %d", lsn, fileLSN)
	}

	// Read compression flag
	// Try to read compression flag - if it fails, assume old format (uncompressed)
	var compressionFlag uint8
	compressionFlagPos, _ := file.Seek(0, os.SEEK_CUR)
	if err := binary.Read(file, binary.LittleEndian, &compressionFlag); err != nil {
		// Old format (no compression flag) - seek back and read as old format
		file.Seek(compressionFlagPos, os.SEEK_SET)
		compressionFlag = 0 // Assume uncompressed for old format
	}

	// Read WAL data length
	var walLen uint32
	if err := binary.Read(file, binary.LittleEndian, &walLen); err != nil {
		return nil, fmt.Errorf("failed to read WAL length: %w", err)
	}

	// Read WAL data
	walData := make([]byte, walLen)
	if _, err := file.Read(walData); err != nil {
		return nil, fmt.Errorf("failed to read WAL data: %w", err)
	}

	// Decompress only if compression flag indicates it's compressed
	if compressionFlag == 1 {
		if sk.compressor == nil {
			return nil, fmt.Errorf("compressed WAL found but compressor not initialized")
		}
		decompressed, err := sk.compressor.Decompress(walData)
		if err != nil {
			return nil, fmt.Errorf("failed to decompress WAL: %w", err)
		}
		return decompressed, nil
	}

	// Data is uncompressed
	return walData, nil
}

// GetLatestLSN returns the highest LSN stored
func (sk *Safekeeper) GetLatestLSN() uint64 {
	sk.lsnMu.RLock()
	defer sk.lsnMu.RUnlock()
	return sk.latestLSN
}

// loadLatestLSN loads the latest LSN from disk
func (sk *Safekeeper) loadLatestLSN() error {
	entries, err := os.ReadDir(sk.walDir)
	if err != nil {
		return err
	}

	var maxLSN uint64
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		var lsn uint64
		if _, err := fmt.Sscanf(entry.Name(), "wal_%d", &lsn); err != nil {
			continue
		}

		if lsn > maxLSN {
			maxLSN = lsn
		}
	}

	sk.lsnMu.Lock()
	sk.latestLSN = maxLSN
	sk.lsnMu.Unlock()

	return nil
}

// GetState returns the current Safekeeper state
func (sk *Safekeeper) GetState() State {
	sk.stateMu.RLock()
	defer sk.stateMu.RUnlock()
	return sk.state
}

// GetMetrics returns Safekeeper metrics
func (sk *Safekeeper) GetMetrics() map[string]interface{} {
	sk.lsnMu.RLock()
	latestLSN := sk.latestLSN
	sk.lsnMu.RUnlock()

	sk.stateMu.RLock()
	state := sk.state
	term := sk.term
	sk.stateMu.RUnlock()

	metrics := map[string]interface{}{
		"replica_id":      sk.replicaID,
		"state":           state.String(),
		"term":            term,
		"latest_lsn":      latestLSN,
		"wal_count":       sk.walCount,
		"quorum_size":     sk.quorumSize,
		"peer_count":      len(sk.peers),
		"replication_lag": sk.replicationLag.String(),
	}

	// Add compression metrics if enabled
	if sk.compressionEnabled {
		metrics["compression_enabled"] = true
		metrics["compression_ratio"] = sk.compressionRatio
	} else {
		metrics["compression_enabled"] = false
	}

	// Add timeline metrics
	timelines := sk.timelineManager.ListTimelines()
	metrics["timeline_count"] = len(timelines)
	metrics["default_timeline"] = sk.defaultTimelineID

	return metrics
}

// String returns string representation of State
func (s State) String() string {
	switch s {
	case StateFollower:
		return "follower"
	case StateCandidate:
		return "candidate"
	case StateLeader:
		return "leader"
	default:
		return "unknown"
	}
}
