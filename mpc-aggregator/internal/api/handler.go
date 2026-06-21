package api

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
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
	MeterShare uint64 `json:"meter_share"`
	Proof      []byte `json:"proof"`
	Commitment []byte `json:"commitment"`
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

		// Debug: log incoming payload fields to help diagnose large uint64 shares
		log.Printf("[DEBUG-INCOMING] meter=%s ts=%d share=%d proof_len=%d commitment_len=%d remote=%s",
			payload.MeterID, payload.Timestamp, payload.MeterShare, len(payload.Proof), len(payload.Commitment), r.RemoteAddr)

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

		timestampUint := uint64(payload.Timestamp)
		numericMeterID := stringToUint64(payload.MeterID)

		if !ValidateReplay(string(payload.Commitment), timestampUint) {
			log.Printf("[SECURITY] Replay attack detected or proof expired for meter: %s", payload.MeterID)
			http.Error(w, "Replay attack detected or proof expired", http.StatusUnauthorized)
			return
		}

		err := zkp.VerifyProof(payload.Proof, maxLimit, numericMeterID, timestampUint, payload.Commitment, verifyingKey)
		if err != nil {
			log.Printf("[SECURITY] Invalid proof from %s: %v", payload.MeterID, err)
			http.Error(w, "Cryptographic proof validation failed", http.StatusForbidden)
			return
		}

		MarkAsUsed(string(payload.Commitment))

		isComplete, err := store.AddShare(payload.Timestamp, payload.MeterID, payload.MeterShare)
		if err != nil {
			log.Printf("[ERROR] Failed to add share for meter %s: %v", payload.MeterID, err)
			http.Error(w, "Internal server error during data aggregation", http.StatusInternalServerError)
			return
		}

		if isComplete {
			log.Printf("[SUCCESS] Aggregation complete for timestamp: %d", payload.Timestamp)
			log.Printf("[INFO] Data exported to MPC engine")
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
	// Računa SHA-256 heš i uzima prvih 8 bajtova (big-endian)
	sum := sha256.Sum256([]byte(s))

	return binary.BigEndian.Uint64(sum[:8])
}
