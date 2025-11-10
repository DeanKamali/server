package safekeeper

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"path/filepath"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Backup handles WAL backup to S3-compatible storage
type S3Backup struct {
	client   *s3.Client
	bucket   string
	prefix   string
	enabled  bool
	ctx      context.Context
	mu       sync.Mutex
}

// NewS3Backup creates a new S3 backup handler
func NewS3Backup(cfg S3Config) (*S3Backup, error) {
	if cfg.Bucket == "" {
		return &S3Backup{enabled: false}, nil
	}

	ctx := context.Background()

	// Configure AWS SDK
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKey,
			cfg.SecretKey,
			"",
		)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create S3 client
	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
	})

	return &S3Backup{
		client:  client,
		bucket:  cfg.Bucket,
		prefix:  cfg.Prefix,
		enabled: true,
		ctx:     ctx,
	}, nil
}

// BackupWAL backs up a WAL record to S3
func (s *S3Backup) BackupWAL(lsn uint64, walData []byte) error {
	if !s.enabled {
		return nil // Backup disabled
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.walObjectKey(lsn)

	_, err := s.client.PutObject(s.ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(walData),
		ContentType: aws.String("application/octet-stream"),
		Metadata: map[string]string{
			"lsn": fmt.Sprintf("%d", lsn),
		},
	})

	if err != nil {
		return fmt.Errorf("failed to backup WAL to S3: %w", err)
	}

	log.Printf("WAL LSN %d backed up to S3: %s/%s", lsn, s.bucket, key)
	return nil
}

// RestoreWAL restores a WAL record from S3
func (s *S3Backup) RestoreWAL(lsn uint64) ([]byte, error) {
	if !s.enabled {
		return nil, fmt.Errorf("S3 backup not enabled")
	}

	key := s.walObjectKey(lsn)

	result, err := s.client.GetObject(s.ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to restore WAL from S3: %w", err)
	}
	defer result.Body.Close()

	walData, err := io.ReadAll(result.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read WAL data from S3: %w", err)
	}

	log.Printf("WAL LSN %d restored from S3: %s/%s", lsn, s.bucket, key)
	return walData, nil
}

// walObjectKey generates S3 object key for a WAL record
func (s *S3Backup) walObjectKey(lsn uint64) string {
	if s.prefix != "" {
		return filepath.Join(s.prefix, fmt.Sprintf("wal_%d", lsn))
	}
	return fmt.Sprintf("wal_%d", lsn)
}

// IsEnabled returns whether S3 backup is enabled
func (s *S3Backup) IsEnabled() bool {
	return s.enabled
}

