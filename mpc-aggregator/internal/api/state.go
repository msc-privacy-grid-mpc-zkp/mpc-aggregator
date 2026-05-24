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

// Čuvamo i vreme kada je commitment dodat, kako bismo znali kada da ga obrišemo
type commitmentData struct {
	addedAt time.Time
}

var (
	usedCommitments = make(map[string]commitmentData)
	stateMutex      sync.Mutex
)

// init funkcija se u Go-u pokreće automatski pri startu aplikacije.
// Ovde pokrećemo "čistač" u pozadini (background goroutine).
func init() {
	go cleanupRoutine()
}

func ValidateReplay(commitment string, timestamp uint64) bool {
	if time.Now().Unix()-int64(timestamp) > 300 { // 5 minuta prozor
		return false
	}

	stateMutex.Lock()
	defer stateMutex.Unlock()

	_, exists := usedCommitments[commitment]
	return !exists // Vraća true ako heš NIJE viđen
}

func MarkAsUsed(commitment string) {
	stateMutex.Lock()
	defer stateMutex.Unlock()
	usedCommitments[commitment] = commitmentData{
		addedAt: time.Now(),
	}
}

// cleanupRoutine se budi svakih 5 minuta i briše stare heševe, oslobađajući RAM.
func cleanupRoutine() {
	for {
		time.Sleep(5 * time.Minute)

		stateMutex.Lock()
		now := time.Now()
		for hash, data := range usedCommitments {
			// Ako je heš stariji od 5 minuta, bezbedno je obrisati ga
			if now.Sub(data.addedAt) > 5*time.Minute {
				delete(usedCommitments, hash)
			}
		}
		stateMutex.Unlock()
	}
}

type AggregationSession struct {
	Count  int
	Meters map[string]uint64 // Удели потрошње су већ исправно uint64
}

type MemoryStore struct {
	ctx            context.Context
	mu             sync.Mutex
	Sessions       map[int64]*AggregationSession
	ExpectedMeters int
	NodeID         int
	OutputPath     string
}

func NewMemoryStore(ctx context.Context, expected int, nodeID int, outputPath string) *MemoryStore {
	store := &MemoryStore{
		ctx:            ctx,
		Sessions:       make(map[int64]*AggregationSession),
		ExpectedMeters: expected,
		NodeID:         nodeID,
		OutputPath:     outputPath,
	}

	go store.cleanupStaleSessions()

	return store
}

func (store *MemoryStore) AddShare(timestamp int64, meterID string, share uint64) (bool, error) {
	store.mu.Lock()

	session, exists := store.Sessions[timestamp]
	if !exists {
		session = &AggregationSession{
			Count:  0,
			Meters: make(map[string]uint64),
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

	var metersToExport map[string]uint64 // ПРОМЕЊЕНО: int64 у uint64

	if session.Count == store.ExpectedMeters {
		log.Printf("[DEBUG] Bucket full for timestamp: %d", timestamp)
		var expectedTotal uint64 = 0 // ПРОМЕЊЕНО: int64 у uint64
		for k, v := range session.Meters {
			log.Printf("   -> Contains: %s = %d W", k, v)
			expectedTotal += v
		}
		log.Printf("[DEBUG] Total sum being sent to MPC: %d W", expectedTotal)
		log.Println("-------------------------------------------------")

		metersToExport = make(map[string]uint64, len(session.Meters)) // ПРОМЕЊЕНО: int64 у uint64
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

			staleSessions := make(map[int64]map[string]uint64) // ПРОМЕЊЕНО: унутрашња мапа у uint64

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

// ПРОМЕЊЕНО: параметар meters сада прима map[string]uint64 уместо map[string]int64
func (store *MemoryStore) sendToMPC(timestamp int64, meters map[string]uint64) error {
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
		buf.WriteString(strconv.FormatUint(meters[k], 10)) // ПРОМЕЊЕНО: FormatInt у FormatUint
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
