package cache

import (
	"fmt"
	"sync"
	"time"
)

// PageVersion represents a versioned page
type PageVersion struct {
	Data       []byte
	LSN        uint64
	SpaceID    uint32
	PageNo     uint32
	LastAccess time.Time
}

// PageCache implements an LRU cache for pages
type PageCache struct {
	cache      map[string]*PageVersion
	mu         sync.RWMutex
	maxSize    int
	evictCount int
}

// NewPageCache creates a new page cache
func NewPageCache(maxSize int) *PageCache {
	return &PageCache{
		cache:   make(map[string]*PageVersion),
		maxSize: maxSize,
	}
}

// Get retrieves a page from cache
func (pc *PageCache) Get(spaceID uint32, pageNo uint32, lsn uint64) ([]byte, uint64, bool) {
	key := pc.makeKey(spaceID, pageNo)
	
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	
	version, exists := pc.cache[key]
	if !exists {
		return nil, 0, false
	}
	
	// Check if cached version is acceptable (LSN <= requested)
	if version.LSN > lsn {
		return nil, 0, false
	}
	
	// Update last access time
	version.LastAccess = time.Now()
	
	// Return a copy to prevent modification
	data := make([]byte, len(version.Data))
	copy(data, version.Data)
	
	return data, version.LSN, true
}

// Put stores a page in cache
func (pc *PageCache) Put(spaceID uint32, pageNo uint32, lsn uint64, data []byte) {
	key := pc.makeKey(spaceID, pageNo)
	
	pc.mu.Lock()
	defer pc.mu.Unlock()
	
	// Check if we need to evict
	if len(pc.cache) >= pc.maxSize {
		pc.evictLRU()
	}
	
	// Store new version
	pc.cache[key] = &PageVersion{
		Data:       make([]byte, len(data)),
		LSN:        lsn,
		SpaceID:    spaceID,
		PageNo:     pageNo,
		LastAccess: time.Now(),
	}
	copy(pc.cache[key].Data, data)
}

// evictLRU evicts the least recently used page
func (pc *PageCache) evictLRU() {
	var oldestKey string
	var oldestTime time.Time
	
	for key, version := range pc.cache {
		if oldestKey == "" || version.LastAccess.Before(oldestTime) {
			oldestKey = key
			oldestTime = version.LastAccess
		}
	}
	
	if oldestKey != "" {
		delete(pc.cache, oldestKey)
		pc.evictCount++
	}
}

// makeKey creates a cache key
func (pc *PageCache) makeKey(spaceID uint32, pageNo uint32) string {
	return fmt.Sprintf("%d:%d", spaceID, pageNo)
}

// Stats returns cache statistics
func (pc *PageCache) Stats() map[string]interface{} {
	pc.mu.RLock()
	defer pc.mu.RUnlock()
	
	return map[string]interface{}{
		"size":        len(pc.cache),
		"max_size":    pc.maxSize,
		"evict_count": pc.evictCount,
	}
}

// Clear clears the cache
func (pc *PageCache) Clear() {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	pc.cache = make(map[string]*PageVersion)
}

