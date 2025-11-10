package safekeeper

import (
	"log"
	"sync"
	"time"
)

// Consensus implements Raft-like consensus protocol
type Consensus struct {
	safekeeper *Safekeeper
	
	// Election
	electionTimeout time.Duration
	lastHeartbeat  time.Time
	heartbeatMu    sync.RWMutex
	
	// Voting
	votesReceived map[uint64]int // term -> vote count
	votesMu       sync.Mutex
}

// NewConsensus creates a new consensus manager
func NewConsensus(sk *Safekeeper) *Consensus {
	return &Consensus{
		safekeeper:     sk,
		electionTimeout: 5 * time.Second,
		lastHeartbeat:   time.Now(),
		votesReceived:   make(map[uint64]int),
	}
}

// StartElection starts a leader election
func (c *Consensus) StartElection() error {
	c.safekeeper.stateMu.Lock()
	c.safekeeper.term++
	term := c.safekeeper.term
	c.safekeeper.state = StateCandidate
	c.safekeeper.stateMu.Unlock()
	
	log.Printf("Starting election for term %d", term)
	
	// Vote for ourselves
	votes := 1
	c.votesMu.Lock()
	c.votesReceived[term] = votes
	c.votesMu.Unlock()
	
	// Request votes from peers
	for _, peer := range c.safekeeper.peers {
		voteGranted, err := c.requestVote(peer, term)
		if err != nil {
			log.Printf("Failed to request vote from %s: %v", peer, err)
			continue
		}
		
		if voteGranted {
			votes++
			c.votesMu.Lock()
			c.votesReceived[term] = votes
			c.votesMu.Unlock()
		}
	}
	
	// Check if we have quorum
	c.votesMu.Lock()
	voteCount := c.votesReceived[term]
	c.votesMu.Unlock()
	
	if voteCount >= c.safekeeper.quorumSize {
		// We won the election
		c.safekeeper.stateMu.Lock()
		c.safekeeper.state = StateLeader
		c.safekeeper.stateMu.Unlock()
		
		// Set ourselves as known leader
		c.safekeeper.SetKnownLeader("") // Empty means we are the leader
		
		log.Printf("Elected as leader for term %d", term)
		
		// Start sending heartbeats
		go c.sendHeartbeats()
		
		return nil
	}
	
	// Lost election, become follower
	c.safekeeper.stateMu.Lock()
	c.safekeeper.state = StateFollower
	c.safekeeper.stateMu.Unlock()
	
	log.Printf("Lost election for term %d (got %d votes, need %d)", term, voteCount, c.safekeeper.quorumSize)
	
	return nil
}

// requestVote requests a vote from a peer
func (c *Consensus) requestVote(peerEndpoint string, term uint64) (bool, error) {
	c.safekeeper.lsnMu.RLock()
	lastLogLSN := c.safekeeper.latestLSN
	c.safekeeper.lsnMu.RUnlock()
	
	voteGranted, peerTerm, err := c.safekeeper.peerClient.RequestVote(
		peerEndpoint,
		term,
		c.safekeeper.replicaID,
		lastLogLSN,
		term,
	)
	
	if err != nil {
		return false, err
	}
	
	// Update term if peer has higher term
	if peerTerm > term {
		c.safekeeper.stateMu.Lock()
		if peerTerm > c.safekeeper.term {
			c.safekeeper.term = peerTerm
			c.safekeeper.state = StateFollower
		}
		c.safekeeper.stateMu.Unlock()
		return false, nil
	}
	
	return voteGranted, nil
}

// sendHeartbeats sends periodic heartbeats to followers
func (c *Consensus) sendHeartbeats() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	for range ticker.C {
		c.safekeeper.stateMu.RLock()
		isLeader := c.safekeeper.state == StateLeader
		c.safekeeper.stateMu.RUnlock()
		
		if !isLeader {
			return // No longer leader
		}
		
		// Send heartbeats to all peers
		for _, peer := range c.safekeeper.peers {
			if err := c.sendHeartbeat(peer); err != nil {
				log.Printf("Failed to send heartbeat to %s: %v", peer, err)
			}
		}
	}
}

// sendHeartbeat sends a heartbeat to a peer
func (c *Consensus) sendHeartbeat(peerEndpoint string) error {
	c.safekeeper.lsnMu.RLock()
	latestLSN := c.safekeeper.latestLSN
	c.safekeeper.lsnMu.RUnlock()
	
	c.safekeeper.stateMu.RLock()
	term := c.safekeeper.term
	c.safekeeper.stateMu.RUnlock()
	
	return c.safekeeper.peerClient.SendHeartbeat(peerEndpoint, term, c.safekeeper.replicaID, latestLSN)
}

// ReceiveHeartbeat handles incoming heartbeat from leader
func (c *Consensus) ReceiveHeartbeat(leaderID string, term uint64, latestLSN uint64) error {
	c.heartbeatMu.Lock()
	c.lastHeartbeat = time.Now()
	c.heartbeatMu.Unlock()
	
	c.safekeeper.stateMu.Lock()
	if term > c.safekeeper.term {
		c.safekeeper.term = term
		c.safekeeper.state = StateFollower
	}
	c.safekeeper.stateMu.Unlock()
	
	log.Printf("Received heartbeat from leader %s (term %d, LSN %d)", leaderID, term, latestLSN)
	
	return nil
}

// CheckElectionTimeout checks if election timeout has been reached
func (c *Consensus) CheckElectionTimeout() bool {
	c.heartbeatMu.RLock()
	timeSinceHeartbeat := time.Since(c.lastHeartbeat)
	c.heartbeatMu.RUnlock()
	
	return timeSinceHeartbeat > c.electionTimeout
}

// Start starts the consensus manager
func (c *Consensus) Start() {
	// Start as follower
	c.safekeeper.stateMu.Lock()
	c.safekeeper.state = StateFollower
	c.safekeeper.stateMu.Unlock()
	
	// Start election timeout checker
	go func() {
		ticker := time.NewTicker(1 * time.Second)
		defer ticker.Stop()
		
		for range ticker.C {
			c.safekeeper.stateMu.RLock()
			state := c.safekeeper.state
			c.safekeeper.stateMu.RUnlock()
			
			if state == StateFollower && c.CheckElectionTimeout() {
				// No heartbeat received, start election
				if err := c.StartElection(); err != nil {
					log.Printf("Election failed: %v", err)
				}
			}
		}
	}()
}

