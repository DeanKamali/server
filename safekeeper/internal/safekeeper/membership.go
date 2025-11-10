package safekeeper

import (
	"fmt"
	"sync"
)

// MembershipManager handles dynamic replica membership
type MembershipManager struct {
	peers      []string
	peersMu    sync.RWMutex
	quorumSize int
}

// NewMembershipManager creates a new membership manager
func NewMembershipManager(initialPeers []string) *MembershipManager {
	return &MembershipManager{
		peers:      initialPeers,
		quorumSize: (len(initialPeers)+1)/2 + 1,
	}
}

// AddPeer adds a new peer replica
func (mm *MembershipManager) AddPeer(peerEndpoint string) error {
	mm.peersMu.Lock()
	defer mm.peersMu.Unlock()

	// Check if peer already exists
	for _, peer := range mm.peers {
		if peer == peerEndpoint {
			return fmt.Errorf("peer %s already exists", peerEndpoint)
		}
	}

	mm.peers = append(mm.peers, peerEndpoint)
	mm.quorumSize = (len(mm.peers)+1)/2 + 1

	return nil
}

// RemovePeer removes a peer replica
func (mm *MembershipManager) RemovePeer(peerEndpoint string) error {
	mm.peersMu.Lock()
	defer mm.peersMu.Unlock()

	for i, peer := range mm.peers {
		if peer == peerEndpoint {
			mm.peers = append(mm.peers[:i], mm.peers[i+1:]...)
			mm.quorumSize = (len(mm.peers)+1)/2 + 1
			return nil
		}
	}

	return fmt.Errorf("peer %s not found", peerEndpoint)
}

// GetPeers returns the current list of peers
func (mm *MembershipManager) GetPeers() []string {
	mm.peersMu.RLock()
	defer mm.peersMu.RUnlock()

	peers := make([]string, len(mm.peers))
	copy(peers, mm.peers)
	return peers
}

// GetQuorumSize returns the current quorum size
func (mm *MembershipManager) GetQuorumSize() int {
	mm.peersMu.RLock()
	defer mm.peersMu.RUnlock()

	return mm.quorumSize
}

// UpdatePeers replaces the entire peer list
func (mm *MembershipManager) UpdatePeers(newPeers []string) {
	mm.peersMu.Lock()
	defer mm.peersMu.Unlock()

	mm.peers = newPeers
	mm.quorumSize = (len(mm.peers)+1)/2 + 1
}



