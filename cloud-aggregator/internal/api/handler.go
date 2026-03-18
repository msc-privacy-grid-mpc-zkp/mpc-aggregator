package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/msc-privacy-grid-mpc-zkp/cloud-aggregator/internal/zkp"
)

type ProofPayload struct {
	MeterID    string `json:"meter_id"`
	Timestamp  int64  `json:"timestamp"`
	MeterShare int64  `json:"meter_share"`
	Proof      []byte `json:"proof"`
}

func HandleProof(verifyingKey groth16.VerifyingKey, store *MemoryStore, maxLimit uint64) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload ProofPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		defer func() {
			if err := r.Body.Close(); err != nil {
				log.Printf("[WARNING] Failed to close request body for meter %s: %v\n", payload.MeterID, err)
			}
		}()

		// 1. ZKP Verification
		err := zkp.VerifyProof(payload.Proof, maxLimit, verifyingKey)
		if err != nil {
			log.Printf("[SECURITY ALERT] Invalid proof from %s: %v\n", payload.MeterID, err)
			http.Error(w, "Cryptographic proof validation failed", http.StatusForbidden)
			return
		}

		// 2. Add share and trigger MPC export if bucket is full
		isComplete, err := store.AddShare(payload.Timestamp, payload.MeterID, payload.MeterShare)
		if err != nil {
			log.Printf("[ERROR] Failed to export data to RAM Disk: %v\n", err)
			http.Error(w, "Internal server error during data export", http.StatusInternalServerError)
			return
		}

		if isComplete {
			fmt.Printf("\n=================================================\n")
			fmt.Printf("🚀 [SUCCESS] Aggregation complete for timestamp: %d\n", payload.Timestamp)
			fmt.Printf("📂 DATA EXPORTED TO RAM DISK FOR MP-SPDZ\n")
			fmt.Printf("=================================================\n\n")
		}

		// Optional: Log every successful validation to keep track of progress
		// log.Printf("[API] Validated ZKP from %s\n", payload.MeterID)

		w.WriteHeader(http.StatusOK)
	}
}
