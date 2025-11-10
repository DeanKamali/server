package server

import (
	"fmt"
	"log"

	"github.com/linux/projects/server/page-server/internal/auth"
	"github.com/linux/projects/server/page-server/internal/cache"
	"github.com/linux/projects/server/page-server/internal/snapshots"
	"github.com/linux/projects/server/page-server/internal/storage"
	"github.com/linux/projects/server/page-server/internal/wal"
)

// PageServer implements the HTTP Page Server
type PageServer struct {
	Storage         storage.StorageBackend
	WALProcessor    *wal.WALProcessor
	Cache           *cache.PageCache
	Auth            *auth.AuthMiddleware
	SnapshotManager *snapshots.SnapshotManager
}

// Config holds configuration for creating a PageServer
type Config struct {
	DataDir        string
	CacheSize      int
	StorageType    string
	S3Endpoint     string
	S3Bucket       string
	S3Region       string
	S3AccessKey    string
	S3SecretKey    string
	S3Prefix       string
	S3UseSSL       bool
	APIKey         string
	AuthTokens     string
}

// NewPageServer creates a new Page Server with persistent storage
func NewPageServer(cfg Config) (*PageServer, error) {
	// Create storage backend based on type
	var storageBackend storage.StorageBackend
	var err error
	
	switch cfg.StorageType {
	case "s3":
		if cfg.S3Bucket == "" {
			return nil, fmt.Errorf("s3-bucket is required when using S3 storage")
		}
		if cfg.S3Endpoint == "" {
			return nil, fmt.Errorf("s3-endpoint is required when using S3 storage")
		}
		
		s3Config := storage.S3Config{
			Endpoint:  cfg.S3Endpoint,
			Bucket:    cfg.S3Bucket,
			Region:    cfg.S3Region,
			AccessKey: cfg.S3AccessKey,
			SecretKey: cfg.S3SecretKey,
			Prefix:    cfg.S3Prefix,
			UseSSL:    cfg.S3UseSSL,
		}
		storageBackend, err = storage.NewS3Storage(s3Config)
		if err != nil {
			return nil, fmt.Errorf("failed to create S3 storage: %w", err)
		}
		log.Printf("Using S3 storage backend: bucket=%s endpoint=%s", cfg.S3Bucket, cfg.S3Endpoint)
		
	case "hybrid":
		// Hybrid: Memory (hot) + LFC (warm) + S3 (cold)
		if cfg.S3Bucket == "" {
			return nil, fmt.Errorf("s3-bucket is required when using hybrid storage")
		}
		if cfg.S3Endpoint == "" {
			return nil, fmt.Errorf("s3-endpoint is required when using hybrid storage")
		}
		
		s3Config := storage.S3Config{
			Endpoint:  cfg.S3Endpoint,
			Bucket:    cfg.S3Bucket,
			Region:    cfg.S3Region,
			AccessKey: cfg.S3AccessKey,
			SecretKey: cfg.S3SecretKey,
			Prefix:    cfg.S3Prefix,
			UseSSL:    cfg.S3UseSSL,
		}
		storageBackend, err = storage.NewHybridStorage(cfg.DataDir, cfg.CacheSize, s3Config)
		if err != nil {
			return nil, fmt.Errorf("failed to create hybrid storage: %w", err)
		}
		log.Printf("Using hybrid storage backend (Memory + LFC + S3)")
		log.Printf("  Memory cache: %d pages", cfg.CacheSize)
		log.Printf("  Local disk: %s", cfg.DataDir)
		log.Printf("  S3 bucket: %s", cfg.S3Bucket)
		
	case "file", "":
		// Default: file-based storage
		storageBackend, err = storage.NewFileStorage(cfg.DataDir)
		if err != nil {
			return nil, fmt.Errorf("failed to create storage: %w", err)
		}
		log.Printf("Using file-based storage backend: %s", cfg.DataDir)
		
	default:
		return nil, fmt.Errorf("unknown storage backend: %s (supported: file, s3, hybrid)", cfg.StorageType)
	}
	
	// Create page cache
	pageCache := cache.NewPageCache(cfg.CacheSize)
	
	// Create WAL processor
	walProcessor := wal.NewWALProcessor(storageBackend, pageCache)
	
	// Create auth middleware
	authMiddleware := auth.NewAuthMiddleware(cfg.APIKey, cfg.AuthTokens)
	
	// Create snapshot manager
	snapshotManager, err := snapshots.NewSnapshotManager(cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("failed to create snapshot manager: %w", err)
	}
	
	return &PageServer{
		Storage:         storageBackend,
		WALProcessor:    walProcessor,
		Cache:           pageCache,
		Auth:            authMiddleware,
		SnapshotManager: snapshotManager,
	}, nil
}

