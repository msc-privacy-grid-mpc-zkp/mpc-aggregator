package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"log"
	"math/rand"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"
)

type AggregationSession struct {
	Count  int
	Meters map[string]int64
}

type ProofCacheEntry struct {
	Timestamp int64
}

type ProofCache struct {
	mu                     sync.RWMutex
	cache                  map[string]ProofCacheEntry
	rejectedReplayAttempts int64
}

type MemoryStore struct {
	ctx            context.Context
	mu             sync.Mutex
	Sessions       map[int64]*AggregationSession
	ExpectedMeters int
	NodeID         int
	OutputPath     string
	ProofCache     *ProofCache
}

func NewMemoryStore(ctx context.Context, expected int, nodeID int, outputPath string) *MemoryStore {
	proofCache := &ProofCache{
		cache:                  make(map[string]ProofCacheEntry),
		rejectedReplayAttempts: 0,
	}

	store := &MemoryStore{
		ctx:            ctx,
		Sessions:       make(map[int64]*AggregationSession),
		ExpectedMeters: expected,
		NodeID:         nodeID,
		OutputPath:     outputPath,
		ProofCache:     proofCache,
	}

	go store.cleanupStaleSessions()
	go store.cleanupStaleProofs()

	return store
}

func (store *MemoryStore) AddShare(timestamp int64, meterID string, share int64) (bool, error) {
	store.mu.Lock()

	session, exists := store.Sessions[timestamp]
	if !exists {
		session = &AggregationSession{
			Count:  0,
			Meters: make(map[string]int64),
		}
		store.Sessions[timestamp] = session
	}

	if _, ok := session.Meters[meterID]; ok {
		store.mu.Unlock()
		return false, nil
	}

	session.Meters[meterID] = share
	session.Count++

	log.Printf("[AGGREGATOR] Progress for timestamp %d: %d/%d meters", timestamp, session.Count, store.ExpectedMeters)

	var metersToExport map[string]int64

	if session.Count == store.ExpectedMeters {
		log.Printf("[DEBUG] Bucket full for timestamp: %d", timestamp)
		var expectedTotal int64 = 0
		for k, v := range session.Meters {
			log.Printf("   -> Contains: %s = %d W", k, v)
			expectedTotal += v
		}
		log.Printf("[DEBUG] Total sum being sent to MPC: %d W", expectedTotal)
		log.Println("-------------------------------------------------")

		metersToExport = make(map[string]int64, len(session.Meters))
		for k, v := range session.Meters {
			metersToExport[k] = v
		}

		delete(store.Sessions, timestamp)
	}

	store.mu.Unlock()

	if metersToExport != nil {
		err := store.sendToMPC(timestamp, metersToExport)
		return true, err
	}

	return false, nil
}

func (store *MemoryStore) cleanupStaleSessions() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-store.ctx.Done():
			return
		case <-ticker.C:
			store.mu.Lock()
			now := time.Now().Unix()

			staleSessions := make(map[int64]map[string]int64)

			for timestamp, session := range store.Sessions {
				if now-timestamp > 60 {
					log.Printf("[CLEANUP] Session %d expired with %d/%d meters. Scheduling for MPC...", timestamp, session.Count, store.ExpectedMeters)
					staleSessions[timestamp] = session.Meters
					delete(store.Sessions, timestamp)
				}
			}
			store.mu.Unlock()

			for ts, meters := range staleSessions {
				err := store.sendToMPC(ts, meters)
				if err != nil {
					log.Printf("[CLEANUP ERROR] Error sending expired session %d to MPC: %v", ts, err)
				}
			}
		}
	}
}

func (store *MemoryStore) cleanupStaleProofs() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-store.ctx.Done():
			return
		case <-ticker.C:
			store.ProofCache.mu.Lock()
			now := time.Now().Unix()
			deletedCount := 0

			for proofHash, entry := range store.ProofCache.cache {
				if now-entry.Timestamp > 60 {
					delete(store.ProofCache.cache, proofHash)
					deletedCount++
				}
			}

			if deletedCount > 0 {
				log.Printf("[CLEANUP] Removed %d stale proof entries from cache", deletedCount)
			}
			store.ProofCache.mu.Unlock()
		}
	}
}

// CheckAndAdd atomically checks if a proof hash exists and adds it if not.
// Returns true if the proof is new (not seen before), false if it's a replay.
// This operation is strictly atomic under the mutex lock.
func (cache *ProofCache) CheckAndAdd(proofHash string) bool {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if _, exists := cache.cache[proofHash]; exists {
		cache.rejectedReplayAttempts++
		return false
	}

	cache.cache[proofHash] = ProofCacheEntry{
		Timestamp: time.Now().Unix(),
	}
	return true
}

// GetRejectedReplayAttempts returns the total count of rejected replay attempts.
// This is thread-safe and can be called for metrics/telemetry purposes.
func (cache *ProofCache) GetRejectedReplayAttempts() int64 {
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	return cache.rejectedReplayAttempts
}

func (store *MemoryStore) sendToMPC(timestamp int64, meters map[string]int64) error {
	addr := fmt.Sprintf("mpc-node-%c:9000", 'a'+store.NodeID)
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return fmt.Errorf("[MPC SEND] connect failed to %s: %w", addr, err)
	}
	defer conn.Close()

	if err := conn.SetWriteDeadline(time.Now().Add(10 * time.Second)); err != nil {
		// Non-fatal but log it
		log.Printf("[MPC SEND] warning: set write deadline failed: %v", err)
	}

	keys := make([]string, 0, len(meters))
	for k := range meters {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	const MAX_METERS = 1000
	actualCount := int64(len(meters))

	h := sha256.New()
	binary.Write(h, binary.BigEndian, timestamp)
	seed := binary.BigEndian.Uint64(h.Sum(nil)[:8])

	r := rand.New(rand.NewSource(int64(seed)))

	var share0, share1, share2 int64
	if actualCount <= 0 {
		share0, share1, share2 = 0, 0, 0
	} else {
		// Deterministically split actualCount into three non-negative shares that sum to actualCount
		share0 = r.Int63n(actualCount + 1)
		remaining := actualCount - share0
		share1 = r.Int63n(remaining + 1)
		share2 = actualCount - share0 - share1
	}

	var myNShare int64
	switch store.NodeID {
	case 0:
		myNShare = share0
	case 1:
		myNShare = share1
	case 2:
		myNShare = share2
	}

	// Build the entire payload in-memory to reduce syscalls and avoid partial writes
	var buf bytes.Buffer
	buf.WriteString(strconv.FormatInt(myNShare, 10))
	buf.WriteByte('\n')

	for _, k := range keys {
		buf.WriteString(strconv.FormatInt(meters[k], 10))
		buf.WriteByte('\n')
	}

	zerosToPad := MAX_METERS - int(actualCount)
	if zerosToPad < 0 {
		return fmt.Errorf("[MPC SEND] batch size %d exceeds MAX_METERS %d", actualCount, MAX_METERS)
	}
	for i := 0; i < zerosToPad; i++ {
		buf.WriteString("0\n")
	}

	if _, err := conn.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("[MPC SEND] write to %s failed: %w", addr, err)
	}

	return nil
}
