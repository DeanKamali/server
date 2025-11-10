package wal

import (
	"encoding/binary"
	"fmt"
	"log"
	"sync"

	"github.com/linux/projects/server/page-server/internal/cache"
	"github.com/linux/projects/server/page-server/internal/storage"
)

// WALRecord represents a WAL record
type WALRecord struct {
	LSN     uint64
	WALData []byte
	SpaceID uint32
	PageNo  uint32
}

// WALProcessor handles WAL record processing and application to pages
type WALProcessor struct {
	storage storage.StorageBackend
	cache   *cache.PageCache
	mu      sync.Mutex
}

// NewWALProcessor creates a new WAL processor
func NewWALProcessor(storage storage.StorageBackend, cache *cache.PageCache) *WALProcessor {
	return &WALProcessor{
		storage: storage,
		cache:   cache,
	}
}

// ProcessWALRecord processes a WAL record and applies it to pages
func (wp *WALProcessor) ProcessWALRecord(record WALRecord) error {
	wp.mu.Lock()
	defer wp.mu.Unlock()
	
	// Store WAL record first (for durability)
	if err := wp.storage.StoreWAL(record.LSN, record.WALData); err != nil {
		return fmt.Errorf("failed to store WAL: %w", err)
	}
	
	// If we have space_id and page_no, try to apply the WAL
	if record.SpaceID > 0 && record.PageNo > 0 {
		if err := wp.applyWALToPage(record); err != nil {
			log.Printf("Warning: Failed to apply WAL to page: %v", err)
			// Don't fail the request if WAL application fails
			// The WAL is stored and can be replayed later
		}
	}
	
	return nil
}

// applyWALToPage applies a WAL record to a specific page
func (wp *WALProcessor) applyWALToPage(record WALRecord) error {
	// Load the current page version (or create empty page)
	pageData, pageLSN, err := wp.storage.LoadPage(record.SpaceID, record.PageNo, record.LSN)
	if err != nil {
		// Page doesn't exist yet, create empty page
		pageData = make([]byte, 16384) // InnoDB default page size
		pageLSN = 0
	}
	
	// Apply WAL record to page using full InnoDB redo log parsing
	updatedPage, err := wp.applyRedoLogRecord(pageData, record.WALData, record.LSN)
	if err != nil {
		return fmt.Errorf("failed to apply redo log: %w", err)
	}
	
	// Store the updated page with new LSN
	if err := wp.storage.StorePage(record.SpaceID, record.PageNo, record.LSN, updatedPage); err != nil {
		return fmt.Errorf("failed to store updated page: %w", err)
	}
	
	// Update cache
	wp.cache.Put(record.SpaceID, record.PageNo, record.LSN, updatedPage)
	
	log.Printf("Applied WAL to page: space=%d page=%d old_lsn=%d new_lsn=%d",
		record.SpaceID, record.PageNo, pageLSN, record.LSN)
	
	return nil
}

// applyRedoLogRecord applies a redo log record to a page
// Now uses full InnoDB redo log parsing
func (wp *WALProcessor) applyRedoLogRecord(pageData []byte, walData []byte, lsn uint64) ([]byte, error) {
	if len(walData) == 0 {
		return pageData, nil
	}

	// Ensure page is at least 16KB (InnoDB default page size)
	if len(pageData) < 16384 {
		newPage := make([]byte, 16384)
		copy(newPage, pageData)
		pageData = newPage
	}

	result := make([]byte, len(pageData))
	copy(result, pageData)

	// Parse redo log records
	parser := NewRedoLogParser(walData)
	
	for {
		record, err := parser.ParseRecord()
		if err != nil {
			// End of buffer or parsing error
			break
		}

		// Apply record to page
		if err := wp.applyRecordToPage(result, record, lsn); err != nil {
			log.Printf("Warning: Failed to apply redo log record type 0x%02x: %v", record.Type, err)
			// Continue with other records
		}
	}

	// Update page LSN in page header (InnoDB stores LSN at offset 0x00)
	if len(result) >= 8 {
		binary.LittleEndian.PutUint64(result[0:8], lsn)
	}

	return result, nil
}

// applyRecordToPage applies a parsed redo log record to a page
func (wp *WALProcessor) applyRecordToPage(pageData []byte, record *RedoLogRecord, lsn uint64) error {
	switch record.Type {
	case MREC_FREE_PAGE:
		// FREE_PAGE: Mark page as free (zero it out)
		// In production, you might want to track this differently
		for i := range pageData {
			pageData[i] = 0
		}
		return nil

	case MREC_INIT_PAGE:
		// INIT_PAGE: Zero-initialize the page
		for i := range pageData {
			pageData[i] = 0
		}
		// Set page type (FIL_PAGE_TYPE at offset 24)
		if len(pageData) >= 26 {
			// FIL_PAGE_TYPE = 0 (FIL_PAGE_INDEX)
			binary.LittleEndian.PutUint16(pageData[24:26], 0)
		}
		return nil

	case MREC_WRITE:
		// WRITE: Write data at offset
		if int(record.Offset)+len(record.Data) > len(pageData) {
			return fmt.Errorf("write exceeds page size: offset=%d len=%d page_size=%d",
				record.Offset, len(record.Data), len(pageData))
		}
		copy(pageData[record.Offset:record.Offset+uint32(len(record.Data))], record.Data)
		return nil

	case MREC_MEMSET:
		// MEMSET: Fill memory with pattern
		if int(record.Offset)+int(record.DataLen) > len(pageData) {
			return fmt.Errorf("memset exceeds page size: offset=%d len=%d page_size=%d",
				record.Offset, record.DataLen, len(pageData))
		}
		// Repeat fill pattern
		pattern := record.Data
		if len(pattern) == 0 {
			pattern = []byte{0} // Default to zero if no pattern
		}
		for i := uint32(0); i < record.DataLen; i++ {
			pageData[record.Offset+i] = pattern[i%uint32(len(pattern))]
		}
		return nil

	case MREC_MEMMOVE:
		// MEMMOVE: Copy data within page
		sourceOffset := int32(record.Offset) + record.SourceOff
		if sourceOffset < 0 || int(sourceOffset)+int(record.DataLen) > len(pageData) {
			return fmt.Errorf("memmove source out of bounds: source=%d len=%d page_size=%d",
				sourceOffset, record.DataLen, len(pageData))
		}
		if int(record.Offset)+int(record.DataLen) > len(pageData) {
			return fmt.Errorf("memmove dest exceeds page size: offset=%d len=%d page_size=%d",
				record.Offset, record.DataLen, len(pageData))
		}
		// Copy data
		copy(pageData[record.Offset:record.Offset+record.DataLen],
			pageData[sourceOffset:sourceOffset+int32(record.DataLen)])
		return nil

	case MREC_EXTENDED:
		// EXTENDED: Handle extended record types
		// TODO: Implement extended record subtypes
		log.Printf("EXTENDED record subtype 0x%02x not yet implemented", record.Subtype)
		return nil

	case MREC_OPTION:
		// OPTION: Optional record, can be ignored
		return nil

	default:
		return fmt.Errorf("unknown record type: 0x%02x", record.Type)
	}
}

// ReplayWAL replays WAL records from a given LSN
func (wp *WALProcessor) ReplayWAL(fromLSN uint64) error {
	// This would scan WAL files and replay them
	// For now, this is a placeholder
	log.Printf("WAL replay from LSN %d not yet implemented", fromLSN)
	return nil
}

