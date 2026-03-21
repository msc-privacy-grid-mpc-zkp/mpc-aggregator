package api

import (
	"bufio"
	"fmt"
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
		err := store.sendToMPC(session.Meters)

		// Clear session to free up memory
		delete(store.Sessions, timestamp)

		return true, err
	}

	return false, nil
}

func (store *MemoryStore) sendToMPC(meters map[string]int64) error {
	addr := fmt.Sprintf("mpc-node-%c:9000", 'a'+store.NodeID)

	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return fmt.Errorf("[MPC SEND] connect failed to %s: %w", addr, err)
	}
	defer func() {
		if err := conn.Close(); err != nil {
			fmt.Printf("[MPC SEND] connection close error: %v\n", err)
		}
	}()

	// optional safety: write deadline
	_ = conn.SetWriteDeadline(time.Now().Add(5 * time.Second))

	// sortiranje
	keys := make([]string, 0, len(meters))
	for k := range meters {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// buffer za efikasnost
	writer := bufio.NewWriter(conn)

	for _, k := range keys {
		if _, err := fmt.Fprintf(writer, "%d\n", meters[k]); err != nil {
			return fmt.Errorf("[MPC SEND] write failed (meter %s): %w", k, err)
		}
	}

	// flush je KRITIČAN
	if err := writer.Flush(); err != nil {
		return fmt.Errorf("[MPC SEND] flush failed: %w", err)
	}

	return nil
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
