package api

import (
	"bufio"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"os"
	"path/filepath"
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
	return &MemoryStore{
		Sessions:       make(map[int64]*AggregationSession),
		ExpectedMeters: expected,
		NodeID:         nodeID,
		OutputPath:     outputPath,
	}
}

func (store *MemoryStore) AddShare(timestamp int64, meterID string, share int64) (bool, error) {
	store.mu.Lock()
	defer store.mu.Unlock()

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
		return false, nil
	}

	session.Meters[meterID] = share
	session.Count++

	fmt.Printf("[AGGREGATOR] Progress for timestamp %d: %d/%d meters\n", timestamp, session.Count, store.ExpectedMeters)

	if session.Count == store.ExpectedMeters {
		// As soon as the bucket is full, export to RAM Disk immediately
		// err := store.exportToRAMDisk(session.Meters)
		err := store.sendToMPC(timestamp, session.Meters)

		// Clear session to free up memory
		delete(store.Sessions, timestamp)

		return true, err
	}

	return false, nil
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
	// Koristimo SHA256 od timestampa da dobijemo stabilan seed za PRNG
	h := sha256.New()
	binary.Write(h, binary.BigEndian, timestamp)
	seed := binary.BigEndian.Uint64(h.Sum(nil)[:8])

	// Inicijalizujemo lokalni generator sa zajedničkim seedom
	r := rand.New(rand.NewSource(int64(seed)))

	// Generišemo shares tako da: s0 + s1 + s2 = actualCount
	// Svaki čvor generiše istu sekvencu, ali uzima samo svoj deo
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

// exportToRAMDisk is an internal helper method for MP-SPDZ integration
func (store *MemoryStore) exportToRAMDisk(meters map[string]int64) error {
	fileName := fmt.Sprintf("Input-P%d-0", store.NodeID)
	fullPath := filepath.Join(store.OutputPath, fileName)

	// sort meter IDs
	keys := make([]string, 0, len(meters))
	for k := range meters {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var content string
	for _, k := range keys {
		content += fmt.Sprintf("%d\n", meters[k])
	}

	return os.WriteFile(fullPath, []byte(content), 0644)
}
