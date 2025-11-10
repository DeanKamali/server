package cache

import (
	"fmt"
	"sync"
	"time"
)

// LFCCache implements Neon's Local File Cache (LFC)
// This is a large RAM-based cache that uses up to 75% of available RAM
// It acts as Tier 2 between small memory cache (Tier 1) and S3 (Tier 3)
type LFCCache struct {
	// Cache storage
	cache      map[string]*LFCPage
	mu         sync.RWMutex
	
	// Configuration
	maxSize    int64  // Maximum size in bytes (75% of RAM)
	maxPages   int    // Maximum number of pages
	currentSize int64 // Current size in bytes
	
	// Statistics
	hits       int64
	misses    int64
	evictions int64
}

// LFCPage represents a page in the LFC
type LFCPage struct {
	Data       []byte
	LSN        uint64
	SpaceID    uint32
	PageNo     uint32
	Size       int64
	LastAccess time.Time
	AccessCount int64
}

// NewLFCCache creates a new Local File Cache
// maxSizeBytes: Maximum size in bytes (typically 75% of available RAM)
func NewLFCCache(maxSizeBytes int64) *LFCCache {
	// Estimate max pages (assuming average page size of 16KB)
	avgPageSize := int64(16384) // 16KB
	maxPages := int(maxSizeBytes / avgPageSize)
	if maxPages < 100 {
		maxPages = 100 // Minimum 100 pages
	}
	
	return &LFCCache{
		cache:     make(map[string]*LFCPage),
		maxSize:   maxSizeBytes,
		maxPages:  maxPages,
		currentSize: 0,
	}
}

// Get retrieves a page from LFC
func (lfc *LFCCache) Get(spaceID uint32, pageNo uint32, lsn uint64) ([]byte, uint64, bool) {
	key := lfc.makeKey(spaceID, pageNo)
	
	lfc.mu.RLock()
	page, exists := lfc.cache[key]
	lfc.mu.RUnlock()
	
	if !exists {
		lfc.mu.Lock()
		lfc.misses++
		lfc.mu.Unlock()
		return nil, 0, false
	}
	
	// Check if cached version is acceptable (LSN <= requested)
	if page.LSN > lsn {
		lfc.mu.Lock()
		lfc.misses++
		lfc.mu.Unlock()
		return nil, 0, false
	}
	
	// Update access statistics
	lfc.mu.Lock()
	page.LastAccess = time.Now()
	page.AccessCount++
	lfc.hits++
	lfc.mu.Unlock()
	
	// Return a copy to prevent modification
	data := make([]byte, len(page.Data))
	copy(data, page.Data)
	
	return data, page.LSN, true
}

// Put stores a page in LFC
func (lfc *LFCCache) Put(spaceID uint32, pageNo uint32, lsn uint64, data []byte) {
	key := lfc.makeKey(spaceID, pageNo)
	pageSize := int64(len(data))
	
	lfc.mu.Lock()
	defer lfc.mu.Unlock()
	
	// Check if page already exists (update)
	if existing, exists := lfc.cache[key]; exists {
		// Update existing page
		lfc.currentSize -= existing.Size
		existing.Data = make([]byte, len(data))
		copy(existing.Data, data)
		existing.LSN = lsn
		existing.Size = pageSize
		existing.LastAccess = time.Now()
		existing.AccessCount++
		lfc.currentSize += pageSize
		return
	}
	
	// Check if we need to evict
	for lfc.currentSize+pageSize > lfc.maxSize || len(lfc.cache) >= lfc.maxPages {
		if !lfc.evictLRU() {
			break // Can't evict more
		}
	}
	
	// Check again after eviction
	if lfc.currentSize+pageSize > lfc.maxSize {
		// Still too large, skip this page
		return
	}
	
	// Store new page
	lfc.cache[key] = &LFCPage{
		Data:       make([]byte, len(data)),
		LSN:        lsn,
		SpaceID:    spaceID,
		PageNo:     pageNo,
		Size:       pageSize,
		LastAccess: time.Now(),
		AccessCount: 1,
	}
	copy(lfc.cache[key].Data, data)
	lfc.currentSize += pageSize
}

// evictLRU evicts the least recently used page
func (lfc *LFCCache) evictLRU() bool {
	if len(lfc.cache) == 0 {
		return false
	}
	
	var oldestKey string
	var oldestTime time.Time
	
	for key, page := range lfc.cache {
		if oldestKey == "" || page.LastAccess.Before(oldestTime) {
			oldestKey = key
			oldestTime = page.LastAccess
		}
	}
	
	if oldestKey != "" {
		page := lfc.cache[oldestKey]
		lfc.currentSize -= page.Size
		delete(lfc.cache, oldestKey)
		lfc.evictions++
		return true
	}
	
	return false
}

// makeKey creates a cache key
func (lfc *LFCCache) makeKey(spaceID uint32, pageNo uint32) string {
	return fmt.Sprintf("%d:%d", spaceID, pageNo)
}

// Stats returns LFC statistics
func (lfc *LFCCache) Stats() map[string]interface{} {
	lfc.mu.RLock()
	defer lfc.mu.RUnlock()
	
	return map[string]interface{}{
		"size_bytes":     lfc.currentSize,
		"max_size_bytes": lfc.maxSize,
		"size_pages":     len(lfc.cache),
		"max_pages":      lfc.maxPages,
		"hits":           lfc.hits,
		"misses":         lfc.misses,
		"evictions":      lfc.evictions,
		"hit_rate":       lfc.calculateHitRate(),
	}
}

// calculateHitRate calculates the hit rate
func (lfc *LFCCache) calculateHitRate() float64 {
	total := lfc.hits + lfc.misses
	if total == 0 {
		return 0.0
	}
	return float64(lfc.hits) / float64(total) * 100.0
}

// Clear clears the LFC
func (lfc *LFCCache) Clear() {
	lfc.mu.Lock()
	defer lfc.mu.Unlock()
	
	lfc.cache = make(map[string]*LFCPage)
	lfc.currentSize = 0
}

// GetSize returns current size in bytes
func (lfc *LFCCache) GetSize() int64 {
	lfc.mu.RLock()
	defer lfc.mu.RUnlock()
	return lfc.currentSize
}

// GetMaxSize returns maximum size in bytes
func (lfc *LFCCache) GetMaxSize() int64 {
	return lfc.maxSize
}

