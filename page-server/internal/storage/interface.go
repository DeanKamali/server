package storage

// StorageBackend defines the interface for persistent storage
type StorageBackend interface {
	// StorePage stores a page with its LSN
	StorePage(spaceID uint32, pageNo uint32, lsn uint64, data []byte) error

	// LoadPage loads a page at or before the given LSN
	LoadPage(spaceID uint32, pageNo uint32, lsn uint64) ([]byte, uint64, error)

	// StoreWAL stores a WAL record
	StoreWAL(lsn uint64, data []byte) error

	// GetLatestLSN returns the highest LSN stored
	GetLatestLSN() uint64

	// Close closes the storage backend
	Close() error
}
