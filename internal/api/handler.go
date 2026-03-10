package api

import (
	"encoding/json"
	"fmt"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/msc-privacy-grid-mpc-zkp/cloud-aggregator/internal/zkp"
	"log"
	"net/http"
)

type ProofPayload struct {
	MeterID    string `json:"meter_id"`
	Timestamp  int64  `json:"timestamp"`
	MeterShare uint64 `json:"meter_share"`
	Proof      []byte `json:"proof"`
}

func HandleProof(vk groth16.VerifyingKey, store *MemoryStore) http.HandlerFunc {
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
		defer r.Body.Close()

		const maxLimit uint64 = 10000

		err := zkp.VerifyProof(payload.Proof, maxLimit, vk)
		if err != nil {
			log.Printf("[SECURITY ALERT] Invalid proof from %s: %v\n", payload.MeterID, err)
			http.Error(w, "Cryptographic proof validation failed", http.StatusForbidden)
			return
		}
		// ------------------------------------

		isComplete, average := store.AddShare(payload.Timestamp, payload.MeterID, payload.MeterShare)

		if isComplete {
			fmt.Printf("\n=================================================\n")
			fmt.Printf("🚀 [SUCCESS] Aggregation complete for time: %d\n", payload.Timestamp)
			fmt.Printf("📊 CALCULATED AVERAGE STREET CONSUMPTION: %.2f W\n", average)
			fmt.Printf("=================================================\n\n")
		}

		fmt.Printf("[API] Validated ZKP from %s | Limit: %d\n", payload.MeterID, maxLimit)
		w.WriteHeader(http.StatusOK)
	}
}
