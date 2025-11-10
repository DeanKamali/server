package safekeeper

import (
	"fmt"
	"github.com/klauspost/compress/zstd"
	"io"
)

// Compressor handles WAL compression using Zstd (matching Neon's implementation)
type Compressor struct {
	encoder *zstd.Encoder
	decoder *zstd.Decoder
}

// NewCompressor creates a new Zstd compressor
func NewCompressor() (*Compressor, error) {
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return nil, fmt.Errorf("failed to create Zstd encoder: %w", err)
	}

	decoder, err := zstd.NewReader(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create Zstd decoder: %w", err)
	}

	return &Compressor{
		encoder: encoder,
		decoder: decoder,
	}, nil
}

// Compress compresses WAL data using Zstd
// Returns compressed data and compression ratio
func (c *Compressor) Compress(data []byte) ([]byte, float64, error) {
	if len(data) == 0 {
		return data, 1.0, nil
	}

	compressed := c.encoder.EncodeAll(data, nil)
	ratio := float64(len(compressed)) / float64(len(data))

	return compressed, ratio, nil
}

// Decompress decompresses WAL data using Zstd
func (c *Compressor) Decompress(compressed []byte) ([]byte, error) {
	if len(compressed) == 0 {
		return compressed, nil
	}

	decompressed, err := c.decoder.DecodeAll(compressed, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decompress: %w", err)
	}

	return decompressed, nil
}

// CompressStream compresses data from a reader
func (c *Compressor) CompressStream(reader io.Reader) ([]byte, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, err
	}
	compressed, _, err := c.Compress(data)
	return compressed, err
}

// DecompressStream decompresses data to a writer
func (c *Compressor) DecompressStream(compressed []byte, writer io.Writer) error {
	decompressed, err := c.Decompress(compressed)
	if err != nil {
		return err
	}
	_, err = writer.Write(decompressed)
	return err
}

// Close closes the compressor resources
func (c *Compressor) Close() error {
	if c.encoder != nil {
		c.encoder.Close()
	}
	if c.decoder != nil {
		c.decoder.Close()
	}
	return nil
}



