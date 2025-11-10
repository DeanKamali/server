package proxy

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
)

// MySQL Protocol Constants
const (
	MySQLProtocolVersion = 10
)

// ExtractProjectIDFromConnection extracts project ID from MySQL connection
// This mimics Neon's approach of extracting endpoint/project ID from connection
// Note: This reads from the connection, so the caller must buffer the data
func ExtractProjectIDFromConnection(conn net.Conn) (string, []byte, error) {
	// Read and buffer the initial handshake packets
	// We need to buffer because we'll forward this to the compute node

	// Read first packet (server handshake - we skip this for now as client sends it)
	// Actually, in proxy mode, client connects to us, so we need to read client's initial packet
	// Read first packet header
	header1 := make([]byte, 4)
	if _, err := io.ReadFull(conn, header1); err != nil {
		return "", nil, fmt.Errorf("failed to read packet header: %w", err)
	}

	packetLength1 := uint32(header1[0]) | uint32(header1[1])<<8 | uint32(header1[2])<<16
	packet1 := make([]byte, packetLength1)
	if _, err := io.ReadFull(conn, packet1); err != nil {
		return "", nil, fmt.Errorf("failed to read packet body: %w", err)
	}

	// Parse client response to extract database name
	reader := bytes.NewReader(packet1)

	// Capability flags (4 bytes)
	var clientCapabilities uint32
	binary.Read(reader, binary.LittleEndian, &clientCapabilities)

	// Max packet size (4 bytes)
	reader.Read(make([]byte, 4))

	// Character set (1 byte)
	reader.ReadByte()

	// Reserved (23 bytes)
	reader.Read(make([]byte, 23))

	// Read username (null-terminated)
	for {
		b, err := reader.ReadByte()
		if err != nil || b == 0 {
			break
		}
	}

	// Skip password (for now, assume it's null-terminated or length-encoded)
	// In practice, we'd need to parse it properly based on auth plugin
	for {
		b, err := reader.ReadByte()
		if err != nil || b == 0 {
			break
		}
	}

	// Read database name (null-terminated, if CLIENT_CONNECT_WITH_DB is set)
	var database string
	if clientCapabilities&0x00000008 != 0 {
		dbBytes := make([]byte, 0)
		for {
			b, err := reader.ReadByte()
			if err != nil || b == 0 {
				break
			}
			dbBytes = append(dbBytes, b)
		}
		database = string(dbBytes)
	}

	// Buffer the packets for forwarding
	buffered := append(header1, packet1...)

	if database == "" {
		return "", buffered, fmt.Errorf("no database/project ID in connection")
	}

	return database, buffered, nil
}
