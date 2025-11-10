package storage

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// FileStorage implements file-based persistent storage
type FileStorage struct {
	baseDir    string
	walDir     string
	pagesDir   string
	latestLSN  uint64
	lsnMu      sync.RWMutex
	walMu      sync.Mutex
}

// NewFileStorage creates a new file-based storage backend
func NewFileStorage(baseDir string) (*FileStorage, error) {
	fs := &FileStorage{
		baseDir:  baseDir,
		walDir:   filepath.Join(baseDir, "wal"),
		pagesDir: filepath.Join(baseDir, "pages"),
	}
	
	// Create directories
	if err := os.MkdirAll(fs.walDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory: %w", err)
	}
	if err := os.MkdirAll(fs.pagesDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create pages directory: %w", err)
	}
	
	return fs, nil
}

// StorePage stores a page with versioning
func (fs *FileStorage) StorePage(spaceID uint32, pageNo uint32, lsn uint64, data []byte) error {
	// Create space directory
	spaceDir := filepath.Join(fs.pagesDir, fmt.Sprintf("space_%d", spaceID))
	if err := os.MkdirAll(spaceDir, 0755); err != nil {
		return fmt.Errorf("failed to create space directory: %w", err)
	}
	
	// Page file: page_<pageNo>_<lsn>
	pageFile := filepath.Join(spaceDir, fmt.Sprintf("page_%d_%d", pageNo, lsn))
	
	// Write page data with header: [LSN (8 bytes)][Page Data]
	file, err := os.Create(pageFile)
	if err != nil {
		return fmt.Errorf("failed to create page file: %w", err)
	}
	defer file.Close()
	
	// Write LSN header
	if err := binary.Write(file, binary.LittleEndian, lsn); err != nil {
		return fmt.Errorf("failed to write LSN: %w", err)
	}
	
	// Write page data
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("failed to write page data: %w", err)
	}
	
	// Also create/update latest symlink for quick access
	latestLink := filepath.Join(spaceDir, fmt.Sprintf("page_%d_latest", pageNo))
	os.Remove(latestLink) // Remove old symlink if exists
	os.Symlink(filepath.Base(pageFile), latestLink)
	
	return nil
}

// LoadPage loads a page at or before the given LSN
func (fs *FileStorage) LoadPage(spaceID uint32, pageNo uint32, lsn uint64) ([]byte, uint64, error) {
	spaceDir := filepath.Join(fs.pagesDir, fmt.Sprintf("space_%d", spaceID))
	
	// Try to find the latest version first
	latestLink := filepath.Join(spaceDir, fmt.Sprintf("page_%d_latest", pageNo))
	if target, err := os.Readlink(latestLink); err == nil {
		pageFile := filepath.Join(spaceDir, target)
		if data, pageLSN, err := fs.readPageFile(pageFile, lsn); err == nil {
			return data, pageLSN, nil
		}
	}
	
	// Scan for matching LSN
	pattern := filepath.Join(spaceDir, fmt.Sprintf("page_%d_*", pageNo))
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to glob page files: %w", err)
	}
	
	var bestLSN uint64 = 0
	var bestFile string
	
	for _, match := range matches {
		// Extract LSN from filename: page_<pageNo>_<lsn>
		var fileLSN uint64
		if _, err := fmt.Sscanf(filepath.Base(match), fmt.Sprintf("page_%d_%%d", pageNo), &fileLSN); err != nil {
			continue
		}
		
		// Find the highest LSN <= requested LSN
		if fileLSN <= lsn && fileLSN > bestLSN {
			bestLSN = fileLSN
			bestFile = match
		}
	}
	
	if bestFile == "" {
		return nil, 0, fmt.Errorf("page not found: space=%d page=%d lsn=%d", spaceID, pageNo, lsn)
	}
	
	return fs.readPageFile(bestFile, lsn)
}

// readPageFile reads a page file and returns data and LSN
func (fs *FileStorage) readPageFile(pageFile string, maxLSN uint64) ([]byte, uint64, error) {
	file, err := os.Open(pageFile)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to open page file: %w", err)
	}
	defer file.Close()
	
	// Read LSN
	var pageLSN uint64
	if err := binary.Read(file, binary.LittleEndian, &pageLSN); err != nil {
		return nil, 0, fmt.Errorf("failed to read LSN: %w", err)
	}
	
	// Check if this version is acceptable
	if pageLSN > maxLSN {
		return nil, 0, fmt.Errorf("page LSN %d exceeds requested LSN %d", pageLSN, maxLSN)
	}
	
	// Read page data
	data := make([]byte, 16384) // InnoDB default page size
	n, err := file.Read(data)
	if err != nil && n == 0 {
		return nil, 0, fmt.Errorf("failed to read page data: %w", err)
	}
	
	return data[:n], pageLSN, nil
}

// StoreWAL stores a WAL record
func (fs *FileStorage) StoreWAL(lsn uint64, data []byte) error {
	fs.walMu.Lock()
	defer fs.walMu.Unlock()
	
	// WAL file: wal_<lsn>
	walFile := filepath.Join(fs.walDir, fmt.Sprintf("wal_%d", lsn))
	
	file, err := os.Create(walFile)
	if err != nil {
		return fmt.Errorf("failed to create WAL file: %w", err)
	}
	defer file.Close()
	
	// Write LSN header
	if err := binary.Write(file, binary.LittleEndian, lsn); err != nil {
		return fmt.Errorf("failed to write LSN: %w", err)
	}
	
	// Write WAL data length
	if err := binary.Write(file, binary.LittleEndian, uint32(len(data))); err != nil {
		return fmt.Errorf("failed to write WAL length: %w", err)
	}
	
	// Write WAL data
	if _, err := file.Write(data); err != nil {
		return fmt.Errorf("failed to write WAL data: %w", err)
	}
	
	// Update latest LSN
	fs.lsnMu.Lock()
	if lsn > fs.latestLSN {
		fs.latestLSN = lsn
	}
	fs.lsnMu.Unlock()
	
	return nil
}

// GetLatestLSN returns the highest LSN stored
func (fs *FileStorage) GetLatestLSN() uint64 {
	fs.lsnMu.RLock()
	defer fs.lsnMu.RUnlock()
	return fs.latestLSN
}

// Close closes the storage backend
func (fs *FileStorage) Close() error {
	// File storage doesn't need explicit cleanup
	return nil
}

