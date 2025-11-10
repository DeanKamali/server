package storage

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/linux/projects/server/page-server/internal/cache"
)

// HybridStorage implements Neon's exact tiered caching:
// Tier 1: Small memory cache (PageServer.cache) - Hot data
// Tier 2: Large RAM-based LFC (Local File Cache) - Warm data (up to 75% of RAM)
// Tier 3: S3/Object storage - Cold data
type HybridStorage struct {
	// Tiers
	lfc       *cache.LFCCache // Tier 2: Large RAM-based cache (Neon's LFC)
	s3Storage *S3Storage       // Tier 3: Cold data in S3

	// Optional: Disk storage for persistence (not part of Neon's tiering)
	localDisk *FileStorage // Optional: For WAL persistence only

	// Configuration
	localDir string // Local disk directory (for WAL only)

	// Statistics
	mu              sync.RWMutex
	stats            HybridStats
	promoteThreshold time.Duration // Promote to memory if accessed within this time
}

// HybridStats tracks tiered storage statistics (Neon-style)
type HybridStats struct {
	MemoryHits   int64 // Pages served from Tier 1 (memory cache)
	LFCHits      int64 // Pages served from Tier 2 (LFC - RAM)
	S3Hits       int64 // Pages served from Tier 3 (S3)
	MemoryMisses int64 // Pages not found in memory
	LFCMisses    int64 // Pages not found in LFC
	Promotions   int64 // Pages promoted to higher tier
	Demotions    int64 // Pages demoted to lower tier
}

// NewHybridStorage creates a new hybrid storage with Neon's exact tiered caching
// Note: Memory cache (Tier 1) is managed by PageServer, not here
func NewHybridStorage(localDir string, memorySize int, s3Config S3Config) (*HybridStorage, error) {
	// Get total system memory
	totalRAM := cache.GetSystemMemory()
	
	// LFC uses 75% of total RAM (Neon's exact approach)
	lfcSize := int64(float64(totalRAM) * 0.75)
	if lfcSize < 100*1024*1024 { // Minimum 100MB
		lfcSize = 100 * 1024 * 1024
	}
	
	// Create LFC (Tier 2) - Neon's Local File Cache (RAM-based)
	lfc := cache.NewLFCCache(lfcSize)
	
	// Create S3 storage (Tier 3)
	s3Storage, err := NewS3Storage(s3Config)
	if err != nil {
		return nil, fmt.Errorf("failed to create S3 storage: %w", err)
	}
	
	// Optional: Create disk storage for WAL persistence (not part of tiering)
	var localDisk *FileStorage
	if localDir != "" {
		localDisk, err = NewFileStorage(localDir)
		if err != nil {
			log.Printf("Warning: Failed to create local disk storage for WAL: %v", err)
		}
	}

	hs := &HybridStorage{
		lfc:             lfc,
		s3Storage:       s3Storage,
		localDisk:       localDisk,
		localDir:        localDir,
		promoteThreshold: 5 * time.Minute,
	}

	log.Printf("Hybrid storage initialized (Neon's exact tiered caching):")
	log.Printf("  Tier 1 (Memory): Small cache managed by PageServer (%d pages)", memorySize)
	log.Printf("  Tier 2 (LFC): Large RAM-based cache (%.2f GB, 75%% of RAM)", float64(lfcSize)/(1024*1024*1024))
	log.Printf("  Tier 3 (S3): Cold storage bucket=%s", s3Config.Bucket)
	if localDisk != nil {
		log.Printf("  WAL Persistence: Local disk for WAL only (%s)", localDir)
	}

	return hs, nil
}

// StorePage stores a page using Neon's tiered strategy:
// Note: Tier 1 (Memory) is handled by PageServer.cache.Put()
// 1. Store in LFC (Tier 2, RAM-based, synchronous)
// 2. Store in S3 (Tier 3, async, background)
func (hs *HybridStorage) StorePage(spaceID uint32, pageNo uint32, lsn uint64, data []byte) error {
	// Tier 2: Store in LFC (RAM-based, fast, synchronous)
	hs.lfc.Put(spaceID, pageNo, lsn, data)

	// Tier 3: Store in S3 (async, background)
	// Use goroutine to avoid blocking
	go func() {
		if err := hs.s3Storage.StorePage(spaceID, pageNo, lsn, data); err != nil {
			log.Printf("Warning: Failed to store page in S3: %v", err)
		}
	}()

	return nil
}

// LoadPage loads a page using Neon's exact tiered strategy:
// Note: Tier 1 (Memory) is checked by PageServer before calling this
// 1. Check LFC (Tier 2, RAM-based) - sub-millisecond
// 2. Fetch from S3 (Tier 3) - network latency
// 3. Promote to higher tiers when accessed
func (hs *HybridStorage) LoadPage(spaceID uint32, pageNo uint32, lsn uint64) ([]byte, uint64, error) {
	// Tier 2: Check LFC first (RAM-based, fast)
	pageData, pageLSN, found := hs.lfc.Get(spaceID, pageNo, lsn)
	if found {
		// Found in LFC - PageServer will promote to memory cache (Tier 1)
		hs.mu.Lock()
		hs.stats.LFCHits++
		hs.mu.Unlock()
		return pageData, pageLSN, nil
	}

	hs.mu.Lock()
	hs.stats.LFCMisses++
	hs.mu.Unlock()

	// Tier 3: Fetch from S3
	pageData, pageLSN, err := hs.s3Storage.LoadPage(spaceID, pageNo, lsn)
	if err != nil {
		return nil, 0, err
	}

	// Found in S3 - promote to LFC (PageServer will promote to memory)
	// Store in LFC (Tier 2, RAM) for future access
	hs.lfc.Put(spaceID, pageNo, pageLSN, pageData)

	hs.mu.Lock()
	hs.stats.S3Hits++
	hs.stats.Promotions++
	hs.mu.Unlock()

	return pageData, pageLSN, nil
}

// StoreWAL stores WAL (WAL is not part of tiering, stored for persistence)
// 1. Store on local disk (for local persistence)
// 2. Store in S3 (for durability)
func (hs *HybridStorage) StoreWAL(lsn uint64, data []byte) error {
	// Store on local disk if available (for local persistence)
	if hs.localDisk != nil {
		if err := hs.localDisk.StoreWAL(lsn, data); err != nil {
			log.Printf("Warning: Failed to store WAL on disk: %v", err)
		}
	}

	// Store in S3 (async, background)
	go func() {
		if err := hs.s3Storage.StoreWAL(lsn, data); err != nil {
			log.Printf("Warning: Failed to store WAL in S3: %v", err)
		}
	}()

	return nil
}

// GetLatestLSN returns the highest LSN from S3 (source of truth)
func (hs *HybridStorage) GetLatestLSN() uint64 {
	// S3 is the source of truth for LSN
	return hs.s3Storage.GetLatestLSN()
}

// Close closes all storage tiers
func (hs *HybridStorage) Close() error {
	// Clear LFC (RAM-based, no persistent close needed)
	hs.lfc.Clear()
	
	// Close optional disk storage
	if hs.localDisk != nil {
		if err := hs.localDisk.Close(); err != nil {
			return fmt.Errorf("failed to close local disk: %w", err)
		}
	}
	
	// Close S3 storage
	if err := hs.s3Storage.Close(); err != nil {
		return fmt.Errorf("failed to close S3 storage: %w", err)
	}
	
	return nil
}

// GetStats returns tiered storage statistics
func (hs *HybridStorage) GetStats() HybridStats {
	hs.mu.RLock()
	defer hs.mu.RUnlock()
	return hs.stats
}

// GetLFC returns the LFC cache (for metrics)
func (hs *HybridStorage) GetLFC() *cache.LFCCache {
	return hs.lfc
}

// EvictPage evicts a page from Tier 1 (memory), promoting to Tier 2 (LFC)
// This is called by PageServer when memory cache is full
// Note: Memory cache (Tier 1) is managed by PageServer, not HybridStorage
func (hs *HybridStorage) EvictPage(spaceID uint32, pageNo uint32, pageLSN uint64, pageData []byte) {
	// Promote to LFC (Tier 2, RAM-based) before evicting from memory
	hs.lfc.Put(spaceID, pageNo, pageLSN, pageData)
	
	hs.mu.Lock()
	hs.stats.Demotions++
	hs.mu.Unlock()
}

