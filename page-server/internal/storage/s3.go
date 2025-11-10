package storage

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"strings"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Storage implements StorageBackend using S3-compatible object storage
type S3Storage struct {
	client    *s3.Client
	bucket    string
	prefix    string
	latestLSN uint64
	lsnMu     sync.RWMutex
	walMu     sync.Mutex
	ctx       context.Context
}

// S3Config holds S3 configuration
type S3Config struct {
	Endpoint  string // S3 endpoint (e.g., https://s3.amazonaws.com or http://minio:9000)
	Bucket    string // S3 bucket name
	Region    string // AWS region (e.g., us-east-1)
	AccessKey string // Access key ID
	SecretKey string // Secret access key
	Prefix    string // Optional prefix for all objects
	UseSSL    bool   // Use SSL/TLS (default: true)
}

// NewS3Storage creates a new S3 storage backend
func NewS3Storage(cfg S3Config) (*S3Storage, error) {
	ctx := context.Background()

	// Build AWS config with custom resolver for endpoint
	var awsCfg aws.Config
	var err error

	// If credentials provided, use them; otherwise use default chain
	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		awsCfg, err = config.LoadDefaultConfig(ctx,
			config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
				cfg.AccessKey,
				cfg.SecretKey,
				"",
			)),
			config.WithRegion(cfg.Region),
		)
	} else {
		awsCfg, err = config.LoadDefaultConfig(ctx,
			config.WithRegion(cfg.Region),
		)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client with custom endpoint
	clientOptions := []func(*s3.Options){
		func(o *s3.Options) {
			o.UsePathStyle = true // Required for MinIO and some S3-compatible services
		},
	}

	// Override endpoint if provided (for MinIO or custom S3)
	if cfg.Endpoint != "" {
		clientOptions = append(clientOptions, func(o *s3.Options) {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		})
	}

	client := s3.NewFromConfig(awsCfg, clientOptions...)

	// Ensure bucket exists
	if err := ensureBucketExists(ctx, client, cfg.Bucket); err != nil {
		return nil, fmt.Errorf("failed to ensure bucket exists: %w", err)
	}

	storage := &S3Storage{
		client: client,
		bucket: cfg.Bucket,
		prefix: strings.Trim(cfg.Prefix, "/"),
		ctx:    ctx,
	}

	// Load latest LSN from S3
	if err := storage.loadLatestLSN(); err != nil {
		log.Printf("Warning: Failed to load latest LSN: %v", err)
	}

	return storage, nil
}

// ensureBucketExists creates the bucket if it doesn't exist
func ensureBucketExists(ctx context.Context, client *s3.Client, bucket string) error {
	// Try to head bucket (check if exists)
	_, err := client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	})
	if err == nil {
		return nil // Bucket exists
	}

	// Bucket doesn't exist, create it
	_, err = client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		return fmt.Errorf("failed to create bucket: %w", err)
	}

	log.Printf("Created S3 bucket: %s", bucket)
	return nil
}

// pageObjectKey generates an S3 object key for a page
func (s *S3Storage) pageObjectKey(spaceID uint32, pageNo uint32, lsn uint64) string {
	key := fmt.Sprintf("pages/space_%d/page_%d_%d", spaceID, pageNo, lsn)
	if s.prefix != "" {
		key = filepath.Join(s.prefix, key)
	}
	return key
}

// walObjectKey generates an S3 object key for a WAL record
func (s *S3Storage) walObjectKey(lsn uint64) string {
	key := fmt.Sprintf("wal/wal_%d", lsn)
	if s.prefix != "" {
		key = filepath.Join(s.prefix, key)
	}
	return key
}

// StorePage stores a page in S3
func (s *S3Storage) StorePage(spaceID uint32, pageNo uint32, lsn uint64, data []byte) error {
	key := s.pageObjectKey(spaceID, pageNo, lsn)

	// Prepare page data: [LSN (8 bytes)][Page Data]
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, lsn); err != nil {
		return fmt.Errorf("failed to write LSN: %w", err)
	}
	if _, err := buf.Write(data); err != nil {
		return fmt.Errorf("failed to write page data: %w", err)
	}

	// Upload to S3
	_, err := s.client.PutObject(s.ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(buf.Bytes()),
		ContentType: aws.String("application/octet-stream"),
		Metadata: map[string]string{
			"space-id": fmt.Sprintf("%d", spaceID),
			"page-no":  fmt.Sprintf("%d", pageNo),
			"lsn":      fmt.Sprintf("%d", lsn),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to upload page to S3: %w", err)
	}

	// Update latest LSN
	s.lsnMu.Lock()
	if lsn > s.latestLSN {
		s.latestLSN = lsn
	}
	s.lsnMu.Unlock()

	return nil
}

// LoadPage loads a page from S3 at or before the given LSN
func (s *S3Storage) LoadPage(spaceID uint32, pageNo uint32, lsn uint64) ([]byte, uint64, error) {
	// List objects with prefix to find matching pages
	prefix := fmt.Sprintf("pages/space_%d/page_%d_", spaceID, pageNo)
	if s.prefix != "" {
		prefix = filepath.Join(s.prefix, prefix)
	}

	// List objects
	listInput := &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	}

	var bestLSN uint64 = 0
	var bestKey string

	paginator := s3.NewListObjectsV2Paginator(s.client, listInput)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(s.ctx)
		if err != nil {
			return nil, 0, fmt.Errorf("failed to list objects: %w", err)
		}

		for _, obj := range page.Contents {
			// Extract LSN from key: page_<no>_<lsn>
			var fileLSN uint64
			key := *obj.Key
			baseName := filepath.Base(key)
			if _, err := fmt.Sscanf(baseName, fmt.Sprintf("page_%d_%%d", pageNo), &fileLSN); err != nil {
				continue
			}

			// Find the highest LSN <= requested LSN
			if fileLSN <= lsn && fileLSN > bestLSN {
				bestLSN = fileLSN
				bestKey = key
			}
		}
	}

	if bestKey == "" {
		return nil, 0, fmt.Errorf("page not found: space=%d page=%d lsn=%d", spaceID, pageNo, lsn)
	}

	// Download the object
	return s.downloadPage(bestKey, lsn)
}

// downloadPage downloads a page from S3
func (s *S3Storage) downloadPage(key string, maxLSN uint64) ([]byte, uint64, error) {
	result, err := s.client.GetObject(s.ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, 0, fmt.Errorf("failed to download page: %w", err)
	}
	defer result.Body.Close()

	// Read LSN
	var pageLSN uint64
	if err := binary.Read(result.Body, binary.LittleEndian, &pageLSN); err != nil {
		return nil, 0, fmt.Errorf("failed to read LSN: %w", err)
	}

	// Check if this version is acceptable
	if pageLSN > maxLSN {
		return nil, 0, fmt.Errorf("page LSN %d exceeds requested LSN %d", pageLSN, maxLSN)
	}

	// Read page data
	data, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to read page data: %w", err)
	}

	return data, pageLSN, nil
}

// StoreWAL stores a WAL record in S3
func (s *S3Storage) StoreWAL(lsn uint64, data []byte) error {
	s.walMu.Lock()
	defer s.walMu.Unlock()

	key := s.walObjectKey(lsn)

	// Prepare WAL data: [LSN (8 bytes)][Length (4 bytes)][WAL Data]
	buf := new(bytes.Buffer)
	if err := binary.Write(buf, binary.LittleEndian, lsn); err != nil {
		return fmt.Errorf("failed to write LSN: %w", err)
	}
	if err := binary.Write(buf, binary.LittleEndian, uint32(len(data))); err != nil {
		return fmt.Errorf("failed to write WAL length: %w", err)
	}
	if _, err := buf.Write(data); err != nil {
		return fmt.Errorf("failed to write WAL data: %w", err)
	}

	// Upload to S3
	_, err := s.client.PutObject(s.ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(buf.Bytes()),
		ContentType: aws.String("application/octet-stream"),
		Metadata: map[string]string{
			"lsn": fmt.Sprintf("%d", lsn),
		},
	})
	if err != nil {
		return fmt.Errorf("failed to upload WAL to S3: %w", err)
	}

	// Update latest LSN
	s.lsnMu.Lock()
	if lsn > s.latestLSN {
		s.latestLSN = lsn
	}
	s.lsnMu.Unlock()

	return nil
}

// GetLatestLSN returns the highest LSN stored
func (s *S3Storage) GetLatestLSN() uint64 {
	s.lsnMu.RLock()
	defer s.lsnMu.RUnlock()
	return s.latestLSN
}

// loadLatestLSN scans S3 to find the latest LSN
func (s *S3Storage) loadLatestLSN() error {
	prefix := "wal/wal_"
	if s.prefix != "" {
		prefix = filepath.Join(s.prefix, prefix)
	}

	listInput := &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	}

	var maxLSN uint64 = 0

	paginator := s3.NewListObjectsV2Paginator(s.client, listInput)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(s.ctx)
		if err != nil {
			return fmt.Errorf("failed to list WAL objects: %w", err)
		}

		for _, obj := range page.Contents {
			// Extract LSN from key: wal_<lsn>
			key := *obj.Key
			baseName := filepath.Base(key)
			var lsn uint64
			if _, err := fmt.Sscanf(baseName, "wal_%d", &lsn); err == nil {
				if lsn > maxLSN {
					maxLSN = lsn
				}
			}
		}
	}

	s.lsnMu.Lock()
	s.latestLSN = maxLSN
	s.lsnMu.Unlock()

	return nil
}

// Close closes the S3 storage backend
func (s *S3Storage) Close() error {
	// S3 client doesn't need explicit cleanup
	return nil
}

// ListPages lists all page versions for a given space and page
func (s *S3Storage) ListPages(spaceID uint32, pageNo uint32) ([]uint64, error) {
	prefix := fmt.Sprintf("pages/space_%d/page_%d_", spaceID, pageNo)
	if s.prefix != "" {
		prefix = filepath.Join(s.prefix, prefix)
	}

	listInput := &s3.ListObjectsV2Input{
		Bucket: aws.String(s.bucket),
		Prefix: aws.String(prefix),
	}

	var lsns []uint64

	paginator := s3.NewListObjectsV2Paginator(s.client, listInput)
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(s.ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list pages: %w", err)
		}

		for _, obj := range page.Contents {
			key := *obj.Key
			baseName := filepath.Base(key)
			var lsn uint64
			if _, err := fmt.Sscanf(baseName, fmt.Sprintf("page_%d_%%d", pageNo), &lsn); err == nil {
				lsns = append(lsns, lsn)
			}
		}
	}

	return lsns, nil
}

// DeletePage deletes a specific page version from S3
func (s *S3Storage) DeletePage(spaceID uint32, pageNo uint32, lsn uint64) error {
	key := s.pageObjectKey(spaceID, pageNo, lsn)

	_, err := s.client.DeleteObject(s.ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete page: %w", err)
	}

	return nil
}

