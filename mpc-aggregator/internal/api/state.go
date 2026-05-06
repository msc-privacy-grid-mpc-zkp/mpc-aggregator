package api

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"sort"
	"sync"
	"time"
)

type AggregationSession struct {
	Count  int
	Meters map[string]int64 // Storing meterID -> share to export them individually
}

type MemoryStore struct {
	mu             sync.Mutex
	Sessions       map[int64]*AggregationSession
	ExpectedMeters int
	NodeID         int
	OutputPath     string
}

func NewMemoryStore(expected int, nodeID int, outputPath string) *MemoryStore {
	store := &MemoryStore{
		Sessions:       make(map[int64]*AggregationSession),
		ExpectedMeters: expected,
		NodeID:         nodeID,
		OutputPath:     outputPath,
	}

	go store.cleanupStaleSessions()

	return store
}

func (store *MemoryStore) AddShare(timestamp int64, meterID string, share int64) (bool, error) {
	store.mu.Lock()
	// UKLONJENO: defer store.mu.Unlock() - Otključavaćemo ručno kako bismo oslobodili mrežu

	session, exists := store.Sessions[timestamp]
	if !exists {
		session = &AggregationSession{
			Count:  0,
			Meters: make(map[string]int64),
		}
		store.Sessions[timestamp] = session
	}

	// Prevent duplicates within the same timestamp
	if _, ok := session.Meters[meterID]; ok {
		store.mu.Unlock() // Ručno otključavanje ako izlazimo rano
		return false, nil
	}

	session.Meters[meterID] = share
	session.Count++

	fmt.Printf("[AGGREGATOR] Progress for timestamp %d: %d/%d meters\n", timestamp, session.Count, store.ExpectedMeters)

	var metersToExport map[string]int64

	if session.Count == store.ExpectedMeters {
		// --- 🔍 POČETAK DEBUG LOGA ---
		fmt.Printf("\n🔥 [DEBUG AGGREGATOR] Kanta je PUNA za Timestamp: %d\n", timestamp)
		var expectedTotal int64 = 0
		for k, v := range session.Meters {
			fmt.Printf("   -> Sadrži: %s = %d W\n", k, v)
			expectedTotal += v
		}
		fmt.Printf("🔥 [DEBUG AGGREGATOR] UKUPAN ZBIR KOJI ŠALJEM U MPC: %d W\n", expectedTotal)
		fmt.Printf("-------------------------------------------------\n")
		// --- 🔍 KRAJ DEBUG LOGA ---

		// Duboko kopiramo mapu kako bismo oslobodili Mutex pre mrežnog poziva
		metersToExport = make(map[string]int64)
		for k, v := range session.Meters {
			metersToExport[k] = v
		}

		// Clear session to free up memory
		delete(store.Sessions, timestamp)
	}

	// 🔓 OTKLJUČAVAMO MUTEX PRE MREŽNOG POZIVA
	store.mu.Unlock()

	// Ako imamo podatke za eksport, šaljemo ih SADA, kada memorija više nije blokirana
	if metersToExport != nil {
		err := store.sendToMPC(timestamp, metersToExport)
		return true, err
	}

	return false, nil
}

// cleanupStaleSessions periodically checks and cleans up unfinished sessions
func (store *MemoryStore) cleanupStaleSessions() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		store.mu.Lock()
		now := time.Now().Unix()

		// Skupljamo istekle sesije u privremenu mapu da ne bismo blokirali Mutex tokom slanja
		staleSessions := make(map[int64]map[string]int64)

		for timestamp, session := range store.Sessions {
			// Ako je sesija starija od 60 sekundi (timeout)
			if now-timestamp > 60 {
				fmt.Printf("[CLEANUP] Sesija %d je istekla sa %d/%d brojila. Skupljam za MPC...\n", timestamp, session.Count, store.ExpectedMeters)

				staleSessions[timestamp] = session.Meters
				delete(store.Sessions, timestamp) // Oslobađanje memorije
			}
		}
		// 🔓 OTKLJUČAVAMO MUTEX
		store.mu.Unlock()

		// Šaljemo istekle sesije preko mreže
		for ts, meters := range staleSessions {
			err := store.sendToMPC(ts, meters)
			if err != nil {
				fmt.Printf("[CLEANUP ERROR] Greška pri slanju istekle sesije %d u MPC: %v\n", ts, err)
			}
		}
	}
}

func (store *MemoryStore) sendToMPC(timestamp int64, meters map[string]int64) error {
	addr := fmt.Sprintf("mpc-node-%c:9000", 'a'+store.NodeID)
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return fmt.Errorf("[MPC SEND] connect failed to %s: %w", addr, err)
	}
	defer conn.Close()

	_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

	// 1. Deterministički Sort (Mora biti isti na svim nodovima)
	keys := make([]string, 0, len(meters))
	for k := range meters {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	const MAX_METERS = 1000
	actualCount := int64(len(meters))

	// 2. Kriptografski "Blind" faktori zasnovani na timestampu
	h := sha256.New()
	binary.Write(h, binary.BigEndian, timestamp)
	seed := binary.BigEndian.Uint64(h.Sum(nil)[:8])

	r := rand.New(rand.NewSource(int64(seed)))

	share0 := r.Int63n(1000000)
	share1 := r.Int63n(1000000)
	share2 := actualCount - share0 - share1

	var myNShare int64
	switch store.NodeID {
	case 0:
		myNShare = share0
	case 1:
		myNShare = share1
	case 2:
		myNShare = share2
	}

	writer := bufio.NewWriter(conn)

	// --- KORAK 1: Slanje udela broja N ---
	if _, err := fmt.Fprintf(writer, "%d\n", myNShare); err != nil {
		return fmt.Errorf("[MPC SEND] write N-share failed: %w", err)
	}

	// --- KORAK 2: Slanje udela potrošnje ---
	for _, k := range keys {
		if _, err := fmt.Fprintf(writer, "%d\n", meters[k]); err != nil {
			return fmt.Errorf("[MPC SEND] write failed (meter %s): %w", k, err)
		}
	}

	// --- KORAK 3: Zero Padding ---
	zerosToPad := MAX_METERS - int(actualCount)
	if zerosToPad < 0 {
		return fmt.Errorf("[MPC SEND] batch size %d exceeds MAX_METERS %d", actualCount, MAX_METERS)
	}
	for i := 0; i < zerosToPad; i++ {
		if _, err := fmt.Fprintf(writer, "0\n"); err != nil {
			return fmt.Errorf("[MPC SEND] write padding zero failed: %w", err)
		}
	}

	return writer.Flush()
}
