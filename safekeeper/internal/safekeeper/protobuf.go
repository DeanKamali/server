package safekeeper

import (
	"encoding/binary"
	"fmt"
)

// ProtobufEncoder handles Protobuf-like binary encoding/decoding for WAL records
// Using a simplified binary format (can be upgraded to full Protobuf later)
type ProtobufEncoder struct {
	enabled bool
}

// NewProtobufEncoder creates a new Protobuf encoder
func NewProtobufEncoder(enabled bool) *ProtobufEncoder {
	return &ProtobufEncoder{
		enabled: enabled,
	}
}

// EncodeWALRecord encodes a WAL record to binary format (Protobuf-like)
// Format: [LSN (8 bytes)][SpaceID (4 bytes)][PageNo (4 bytes)][WALDataLen (4 bytes)][WALData (variable)]
func (pe *ProtobufEncoder) EncodeWALRecord(lsn uint64, walData []byte, spaceID uint32, pageNo uint32) ([]byte, error) {
	if !pe.enabled {
		// Fallback to JSON encoding (handled by API layer)
		return nil, fmt.Errorf("protobuf encoding disabled")
	}

	// Binary encoding (more efficient than JSON)
	buf := make([]byte, 0, 20+len(walData))
	
	// LSN (8 bytes)
	lsnBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(lsnBytes, lsn)
	buf = append(buf, lsnBytes...)
	
	// SpaceID (4 bytes)
	spaceIDBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(spaceIDBytes, spaceID)
	buf = append(buf, spaceIDBytes...)
	
	// PageNo (4 bytes)
	pageNoBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(pageNoBytes, pageNo)
	buf = append(buf, pageNoBytes...)
	
	// WALData length (4 bytes)
	walLenBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(walLenBytes, uint32(len(walData)))
	buf = append(buf, walLenBytes...)
	
	// WALData
	buf = append(buf, walData...)
	
	return buf, nil
}

// DecodeWALRecord decodes a binary-encoded WAL record
func (pe *ProtobufEncoder) DecodeWALRecord(data []byte) (uint64, []byte, uint32, uint32, error) {
	if !pe.enabled {
		return 0, nil, 0, 0, fmt.Errorf("protobuf encoding disabled")
	}

	if len(data) < 20 {
		return 0, nil, 0, 0, fmt.Errorf("invalid data length: %d", len(data))
	}

	// LSN (8 bytes)
	lsn := binary.LittleEndian.Uint64(data[0:8])
	
	// SpaceID (4 bytes)
	spaceID := binary.LittleEndian.Uint32(data[8:12])
	
	// PageNo (4 bytes)
	pageNo := binary.LittleEndian.Uint32(data[12:16])
	
	// WALData length (4 bytes)
	walLen := binary.LittleEndian.Uint32(data[16:20])
	
	if len(data) < 20+int(walLen) {
		return 0, nil, 0, 0, fmt.Errorf("invalid data length: expected %d, got %d", 20+int(walLen), len(data))
	}
	
	// WALData
	walData := make([]byte, walLen)
	copy(walData, data[20:20+int(walLen)])
	
	return lsn, walData, spaceID, pageNo, nil
}

// IsEnabled returns whether Protobuf encoding is enabled
func (pe *ProtobufEncoder) IsEnabled() bool {
	return pe.enabled
}

