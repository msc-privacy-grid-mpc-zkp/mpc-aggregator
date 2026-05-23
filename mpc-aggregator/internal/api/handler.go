package api

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"io"
	"log"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/msc-privacy-grid-mpc-zkp/mpc-aggregator/internal/config"
	"github.com/msc-privacy-grid-mpc-zkp/mpc-aggregator/internal/zkp"
)

type ProofPayload struct {
	MeterID    string `json:"meter_id"`
	Timestamp  int64  `json:"timestamp"`
	MeterShare int64  `json:"meter_share"`
	Proof      []byte `json:"proof"`
}

type ResultPayload struct {
	NodeID   int     `json:"node_id"`
	Mean     float64 `json:"mean"`
	Variance float64 `json:"variance"`
}

var verifySemaphore = make(chan struct{}, runtime.NumCPU())

// Helper: read request body with size limit and safe close
func readLimitedBody(w http.ResponseWriter, r *http.Request, maxBytes int64) ([]byte, error) {
	bodyReader := http.MaxBytesReader(w, r.Body, maxBytes)
	defer func() {
		if err := bodyReader.Close(); err != nil {
			log.Printf("[WARNING] Failed to close request body reader: %v", err)
		}
	}()
	data, err := io.ReadAll(bodyReader)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Helper: extract client certificate CN from TLS connection
func getClientCN(r *http.Request) (string, error) {
	if r.TLS == nil {
		return "", fmt.Errorf("no TLS connection")
	}
	if len(r.TLS.PeerCertificates) == 0 {
		return "", fmt.Errorf("no peer certificates")
	}
	cn := strings.TrimSpace(r.TLS.PeerCertificates[0].Subject.CommonName)
	return cn, nil
}

// Helper: authorize that CN maps to payload.NodeID according to cfg
func authorizeClientCN(cfg *config.AppConfig, cn string, reportedNodeID int) error {
	if cfg == nil || cfg.ClientCNToNode == nil {
		return fmt.Errorf("no CN->NodeID mapping configured")
	}
	expectedID, ok := cfg.ClientCNToNode[cn]
	if !ok {
		return fmt.Errorf("unknown client CN=%s", cn)
	}
	if expectedID != reportedNodeID {
		return fmt.Errorf("client CN=%s not authorized for NodeID=%d (expected %d)", cn, reportedNodeID, expectedID)
	}
	return nil
}

func HandleProof(verifyingKey groth16.VerifyingKey, store *MemoryStore, maxLimit uint64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload ProofPayload
		d := json.NewDecoder(r.Body)
		if err := d.Decode(&payload); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		defer func() {
			if err := r.Body.Close(); err != nil {
				log.Printf("[WARNING] Failed to close request body for meter %s: %v", payload.MeterID, err)
			}
		}()

		// Try to acquire verification slot with timeout
		select {
		case verifySemaphore <- struct{}{}:
			defer func() { <-verifySemaphore }()
		case <-time.After(5 * time.Second):
			http.Error(w, "Server busy, try later", http.StatusServiceUnavailable)
			return
		}

		// ZKP Integrity: Reject empty proofs immediately before cache check to save CPU cycles
		if len(payload.Proof) == 0 {
			log.Printf("[SECURITY] EMPTY PROOF REJECTED: meter=%s, timestamp=%d, remote_addr=%s", payload.MeterID, payload.Timestamp, r.RemoteAddr)
			http.Error(w, "Empty proof not allowed", http.StatusBadRequest)
			return
		}

		// Convert timestamp to uint64 for ZKP verification
		// Must match the exact value used during proof generation in smart-meter-simulator
		timestampUint := uint64(payload.Timestamp)
		numericMeterID := stringToUint64(payload.MeterID)

		// Verify proof with cryptographic binding and freshness check
		// The circuit ensures MeterID and Timestamp are cryptographically bound to the proof.
		// The verifier also checks that the timestamp is within +/- 60 seconds of current time.
		// If an attacker replays a proof with a stale timestamp, verification will fail.
		err := zkp.VerifyProof(payload.Proof, maxLimit, numericMeterID, timestampUint, verifyingKey)
		if err != nil {
			log.Printf("[SECURITY] Invalid proof from %s: %v", payload.MeterID, err)
			http.Error(w, "Cryptographic proof validation failed", http.StatusForbidden)
			return
		}

		// Compute proof hash for replay attack detection (constant-time friendly SHA-256)
		// This provides an additional layer of protection against replay attacks
		proofHash := computeProofHash(payload.Proof, payload.MeterID, payload.Timestamp)

		// Check if proof has been seen before (replay attack detection) - atomic operation
		if !store.ProofCache.CheckAndAdd(proofHash) {
			log.Printf("[SECURITY] REPLAY ATTACK DETECTED: meter=%s, timestamp=%d, remote_addr=%s, total_rejected=%d", payload.MeterID, payload.Timestamp, r.RemoteAddr, store.ProofCache.GetRejectedReplayAttempts())
			http.Error(w, "Duplicate proof detected", http.StatusForbidden)
			return
		}

		isComplete, err := store.AddShare(payload.Timestamp, payload.MeterID, payload.MeterShare)
		if err != nil {
			log.Printf("[ERROR] Failed to export data to RAM disk: %v", err)
			http.Error(w, "Internal server error during data export", http.StatusInternalServerError)
			return
		}

		if isComplete {
			log.Printf("[SUCCESS] Aggregation complete for timestamp: %d", payload.Timestamp)
			log.Printf("[INFO] Data exported to RAM disk for MP-SPDZ")
		}

		w.WriteHeader(http.StatusOK)
	}
}

func HandleMPCResults(scaleFactor float64, cfg *config.AppConfig) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		data, err := readLimitedBody(w, r, 1<<20)
		if err != nil {
			log.Printf("[ERROR] Failed to read MPC results body: %v", err)
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		// TLS / client cert checks
		cn, err := getClientCN(r)
		if err != nil {
			log.Printf("[DEBUG] TLS client cert check failed: %v", err)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		log.Printf("[INFO] MPC results received from client cert CN=%s (RemoteAddr=%s)", cn, r.RemoteAddr)

		var payload ResultPayload
		if err := json.Unmarshal(data, &payload); err != nil {
			log.Printf("[ERROR] Failed to unmarshal MPC results: %v", err)
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		if err := authorizeClientCN(cfg, cn, payload.NodeID); err != nil {
			// Distinguish between missing config and explicit authorization failures
			if cfg == nil || cfg.ClientCNToNode == nil {
				log.Printf("[WARNING] No client CN->NodeID mapping configured; rejecting for safety")
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
			// known CN but mismatch
			log.Printf("[SECURITY] %v", err)
			http.Error(w, "Forbidden", http.StatusForbidden)
			return
		}

		realMean := payload.Mean * scaleFactor
		realVariance := payload.Variance * (scaleFactor * scaleFactor)

		log.Printf("[MPC RESULT] Node %d reported final results; Mean=%.2f W; Variance=%.2f", payload.NodeID, realMean, realVariance)

		w.WriteHeader(http.StatusOK)
	}
}

func stringToUint64(s string) uint64 {
	h := fnv.New64a()
	if _, err := h.Write([]byte(s)); err != nil {
		log.Printf("[WARNING] Failed to write to hash: %v", err)
	}
	return h.Sum64()
}

// computeProofHash computes a SHA-256 hash of the proof, meterID, and timestamp.
// SHA-256 is constant-time and cryptographically secure, making it suitable for
// replay attack detection. The hash combines all three components to ensure uniqueness.
func computeProofHash(proofBytes []byte, meterID string, timestamp int64) string {
	h := sha256.New()
	// Write proof bytes (variable length)
	h.Write(proofBytes)
	// Write meter ID as UTF-8 bytes
	h.Write([]byte(meterID))
	// Write timestamp as big-endian 64-bit integer (constant 8 bytes)
	binary.Write(h, binary.BigEndian, timestamp)
	// Return hex-encoded digest (constant-time friendly)
	return hex.EncodeToString(h.Sum(nil))
}
