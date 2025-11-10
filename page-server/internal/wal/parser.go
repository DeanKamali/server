package wal

import (
	"fmt"
)

// Redo log record types (from InnoDB mtr0types.h)
const (
	MREC_FREE_PAGE = 0x00 // Free a page
	MREC_INIT_PAGE = 0x10 // Zero-initialize a page
	MREC_EXTENDED  = 0x20 // Extended record with subtype
	MREC_WRITE     = 0x30 // Write a string of bytes
	MREC_MEMSET    = 0x40 // Write repeated bytes
	MREC_MEMMOVE   = 0x50 // Copy data within page
	MREC_RESERVED  = 0x60 // Reserved
	MREC_OPTION    = 0x70 // Optional record
)

// RedoLogRecord represents a parsed InnoDB redo log record
type RedoLogRecord struct {
	Type      byte   // Record type
	SamePage  bool   // Same page as previous record
	SpaceID   uint32 // Tablespace ID
	PageNo    uint32 // Page number
	Offset    uint32 // Byte offset on page
	Data      []byte // Data to write
	DataLen   uint32 // Length for MEMSET
	SourceOff int32  // Source offset for MEMMOVE (signed)
	Subtype   byte   // Subtype for EXTENDED records
}

// RedoLogParser parses InnoDB redo log records
type RedoLogParser struct {
	pos      int    // Current position in buffer
	buf      []byte // Buffer to parse
	lastPage struct {
		spaceID uint32
		pageNo  uint32
		offset  uint32
	}
}

// NewRedoLogParser creates a new redo log parser
func NewRedoLogParser(data []byte) *RedoLogParser {
	return &RedoLogParser{
		buf: data,
		pos: 0,
	}
}

// ParseRecord parses a single redo log record
func (p *RedoLogParser) ParseRecord() (*RedoLogRecord, error) {
	if p.pos >= len(p.buf) {
		return nil, fmt.Errorf("end of buffer")
	}

	// Read first byte
	firstByte := p.buf[p.pos]
	p.pos++

	// Extract flags and type
	samePage := (firstByte & 0x80) != 0
	recordType := firstByte & 0x70
	lengthBits := firstByte & 0x0F

	// Parse length
	length, err := p.parseLength(lengthBits)
	if err != nil {
		return nil, fmt.Errorf("failed to parse length: %w", err)
	}

	record := &RedoLogRecord{
		Type:     recordType,
		SamePage: samePage,
	}

	// Track start position for length calculation
	recordStartPos := p.pos

	// Parse page identifier if not same page
	if !samePage {
		spaceID, err := p.parseVarLenUint32()
		if err != nil {
			return nil, fmt.Errorf("failed to parse space_id: %w", err)
		}
		pageNo, err := p.parseVarLenUint32()
		if err != nil {
			return nil, fmt.Errorf("failed to parse page_no: %w", err)
		}
		record.SpaceID = spaceID
		record.PageNo = pageNo
		p.lastPage.spaceID = spaceID
		p.lastPage.pageNo = pageNo
		p.lastPage.offset = 0
	} else {
		record.SpaceID = p.lastPage.spaceID
		record.PageNo = p.lastPage.pageNo
	}

	// Parse record-specific data
	switch recordType {
	case MREC_FREE_PAGE:
		// FREE_PAGE: no additional data needed
		return record, nil

	case MREC_INIT_PAGE:
		// INIT_PAGE: reset offset to FIL_PAGE_TYPE (offset 24)
		p.lastPage.offset = 24
		record.Offset = 24
		return record, nil

	case MREC_WRITE:
		// WRITE: offset (relative) + data
		offsetDelta, err := p.parseVarLenUint32()
		if err != nil {
			return nil, fmt.Errorf("failed to parse offset: %w", err)
		}
		p.lastPage.offset += offsetDelta
		record.Offset = p.lastPage.offset

		// Remaining bytes in the record are data
		// length is total bytes after first byte
		consumedBytes := p.pos - recordStartPos
		dataLen := int(length) - consumedBytes
		if dataLen > 0 && p.pos+dataLen <= len(p.buf) {
			record.Data = make([]byte, dataLen)
			copy(record.Data, p.buf[p.pos:p.pos+dataLen])
			p.pos += dataLen
			p.lastPage.offset += uint32(dataLen)
		} else if dataLen < 0 {
			return nil, fmt.Errorf("invalid WRITE record: length=%d consumed=%d", length, consumedBytes)
		}
		return record, nil

	case MREC_MEMSET:
		// MEMSET: offset (relative) + length + fill byte(s)
		offsetDelta, err := p.parseVarLenUint32()
		if err != nil {
			return nil, fmt.Errorf("failed to parse offset: %w", err)
		}
		p.lastPage.offset += offsetDelta
		record.Offset = p.lastPage.offset

		// Parse data length (stored as length-1)
		dataLenEncoded, err := p.parseVarLenUint32()
		if err != nil {
			return nil, fmt.Errorf("failed to parse data length: %w", err)
		}
		record.DataLen = dataLenEncoded + 1 // +1 because length is stored as length-1

		// Remaining bytes are fill pattern
		consumedBytes := p.pos - recordStartPos
		patternLen := int(length) - consumedBytes
		if patternLen > 0 && p.pos+patternLen <= len(p.buf) {
			record.Data = make([]byte, patternLen)
			copy(record.Data, p.buf[p.pos:p.pos+patternLen])
			p.pos += patternLen
		} else if patternLen < 0 {
			return nil, fmt.Errorf("invalid MEMSET record: length=%d consumed=%d", length, consumedBytes)
		}
		p.lastPage.offset += record.DataLen
		return record, nil

	case MREC_MEMMOVE:
		// MEMMOVE: offset (relative) + length + source offset (signed)
		offsetDelta, err := p.parseVarLenUint32()
		if err != nil {
			return nil, fmt.Errorf("failed to parse offset: %w", err)
		}
		p.lastPage.offset += offsetDelta
		record.Offset = p.lastPage.offset

		// Parse data length (stored as length-1)
		dataLenEncoded, err := p.parseVarLenUint32()
		if err != nil {
			return nil, fmt.Errorf("failed to parse data length: %w", err)
		}
		record.DataLen = dataLenEncoded + 1

		// Parse source offset (signed, relative)
		sourceOff, err := p.parseVarLenInt32()
		if err != nil {
			return nil, fmt.Errorf("failed to parse source offset: %w", err)
		}
		record.SourceOff = sourceOff
		p.lastPage.offset += record.DataLen
		return record, nil

	case MREC_EXTENDED:
		// EXTENDED: subtype + page identifier (if not same_page) + subtype-specific data
		subtype, err := p.readByte()
		if err != nil {
			return nil, fmt.Errorf("failed to read subtype: %w", err)
		}
		record.Subtype = subtype
		// TODO: Handle extended subtypes
		return record, nil

	default:
		return nil, fmt.Errorf("unknown record type: 0x%02x", recordType)
	}
}

// parseLength parses the length field (variable length encoding)
func (p *RedoLogParser) parseLength(lengthBits byte) (int, error) {
	if lengthBits == 0 {
		// Length is encoded in 1-3 additional bytes
		if p.pos >= len(p.buf) {
			return 0, fmt.Errorf("unexpected end of buffer")
		}
		firstLenByte := p.buf[p.pos]
		p.pos++

		if (firstLenByte & 0x80) == 0 {
			// 0xxxxxxx: 0-127 (total: 16-143 bytes)
			return 16 + int(firstLenByte), nil
		} else if (firstLenByte & 0xC0) == 0x80 {
			// 10xxxxxx: 128-16511 (total: 144-16527 bytes)
			if p.pos >= len(p.buf) {
				return 0, fmt.Errorf("unexpected end of buffer")
			}
			secondByte := p.buf[p.pos]
			p.pos++
			value := int(firstLenByte&0x3F)<<8 | int(secondByte)
			return 144 + value, nil
		} else if (firstLenByte & 0xE0) == 0xC0 {
			// 110xxxxx: 16512-2113663 (total: 16528-2113679 bytes)
			if p.pos+2 > len(p.buf) {
				return 0, fmt.Errorf("unexpected end of buffer")
			}
			secondByte := p.buf[p.pos]
			thirdByte := p.buf[p.pos+1]
			p.pos += 2
			value := int(firstLenByte&0x1F)<<16 | int(secondByte)<<8 | int(thirdByte)
			return 16528 + value, nil
		} else {
			return 0, fmt.Errorf("reserved length encoding")
		}
	}
	// Length is 1-15 bytes (stored directly in bits 3-0)
	return int(lengthBits), nil
}

// parseVarLenUint32 parses a variable-length encoded uint32
func (p *RedoLogParser) parseVarLenUint32() (uint32, error) {
	if p.pos >= len(p.buf) {
		return 0, fmt.Errorf("unexpected end of buffer")
	}

	firstByte := p.buf[p.pos]
	p.pos++

	if (firstByte & 0x80) == 0 {
		// 0xxxxxxx: 0-127
		return uint32(firstByte), nil
	} else if (firstByte & 0xC0) == 0x80 {
		// 10xxxxxx xxxxxxxx: 128-16511
		if p.pos >= len(p.buf) {
			return 0, fmt.Errorf("unexpected end of buffer")
		}
		secondByte := p.buf[p.pos]
		p.pos++
		value := uint32(firstByte&0x3F)<<8 | uint32(secondByte)
		return 128 + value, nil
	} else if (firstByte & 0xE0) == 0xC0 {
		// 110xxxxx xxxxxxxx xxxxxxxx: 16512-2113663
		if p.pos+2 > len(p.buf) {
			return 0, fmt.Errorf("unexpected end of buffer")
		}
		secondByte := p.buf[p.pos]
		thirdByte := p.buf[p.pos+1]
		p.pos += 2
		value := uint32(firstByte&0x1F)<<16 | uint32(secondByte)<<8 | uint32(thirdByte)
		return 16512 + value, nil
	} else if (firstByte & 0xF0) == 0xE0 {
		// 1110xxxx xxxxxxxx xxxxxxxx xxxxxxxx: 2113664-270549119
		if p.pos+3 > len(p.buf) {
			return 0, fmt.Errorf("unexpected end of buffer")
		}
		secondByte := p.buf[p.pos]
		thirdByte := p.buf[p.pos+1]
		fourthByte := p.buf[p.pos+2]
		p.pos += 3
		value := uint32(firstByte&0x0F)<<24 | uint32(secondByte)<<16 | uint32(thirdByte)<<8 | uint32(fourthByte)
		return 2113664 + value, nil
	} else if (firstByte & 0xF8) == 0xF0 {
		// 11110xxx xxxxxxxx xxxxxxxx xxxxxxxx xxxxxxxx: 270549120-34630287487
		if p.pos+4 > len(p.buf) {
			return 0, fmt.Errorf("unexpected end of buffer")
		}
		secondByte := p.buf[p.pos]
		thirdByte := p.buf[p.pos+1]
		fourthByte := p.buf[p.pos+2]
		fifthByte := p.buf[p.pos+3]
		p.pos += 4
		value := uint32(firstByte&0x07)<<32 | uint32(secondByte)<<24 | uint32(thirdByte)<<16 | uint32(fourthByte)<<8 | uint32(fifthByte)
		return 270549120 + value, nil
	} else {
		return 0, fmt.Errorf("reserved encoding")
	}
}

// parseVarLenInt32 parses a variable-length encoded signed int32 (for MEMMOVE source offset)
func (p *RedoLogParser) parseVarLenInt32() (int32, error) {
	if p.pos >= len(p.buf) {
		return 0, fmt.Errorf("unexpected end of buffer")
	}

	firstByte := p.buf[p.pos]
	p.pos++

	// MEMMOVE encoding: (x-1)<<1 for positive, (x-1)<<1|1 for negative
	// Least significant bit is sign: 0=positive, 1=negative
	isNegative := (firstByte & 0x01) != 0
	value := int32(firstByte >> 1)
	value++ // +1 because encoding stores (x-1)

	if isNegative {
		value = -value
	}

	// For larger values, we'd need to read more bytes, but for now
	// we'll handle the common case of 1-byte encoding
	return value, nil
}

// readByte reads a single byte
func (p *RedoLogParser) readByte() (byte, error) {
	if p.pos >= len(p.buf) {
		return 0, fmt.Errorf("unexpected end of buffer")
	}
	b := p.buf[p.pos]
	p.pos++
	return b, nil
}
