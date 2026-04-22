package api

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log"
	"net/http"
	"runtime" // DODATO: Za detekciju broja CPU jezgara

	"github.com/consensys/gnark/backend/groth16"
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

// DODATO: Semafor koji ograničava broj paralelnih ZKP verifikacija.
// runtime.NumCPU() automatski uzima broj logičkih jezgara tvog procesora.
// Ako ti je procesor i dalje zagušen, možeš ručno staviti broj, npr. make(chan struct{}, 4)
var verifySemaphore = make(chan struct{}, runtime.NumCPU())

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

		// --- KONTROLA PARALELIZACIJE ---
		// Zauzimamo jedno "mesto" u semaforu.
		// Ako su sva jezgra zauzeta verifikacijama, ovaj zahtev će strpljivo čekati ovde.
		verifySemaphore <- struct{}{}

		// 1. ZKP Verification
		timestampUint := uint64(payload.Timestamp)

		// 2. Хешујемо стринг ID-а у број (мора бити ИСТА функција као у симулатору!)
		numericMeterID := stringToUint64(payload.MeterID)

		// 3. ZKP Verification са свим параметрима
		err := zkp.VerifyProof(payload.Proof, maxLimit, numericMeterID, timestampUint, verifyingKey)

		// Oslobađamo "mesto" u semaforu čim se provera završi, kako bi sledeći mogao da počne.
		<-verifySemaphore
		// -------------------------------

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

		w.WriteHeader(http.StatusOK)
	}
}

func HandleMPCResults(scaleFactor float64) http.HandlerFunc {
	// ... (ovaj deo ostaje potpuno nepromenjen) ...
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload ResultPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			log.Printf("[ERROR] Failed to decode MPC results: %v\n", err)
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		realMean := payload.Mean * scaleFactor
		realVariance := payload.Variance * (scaleFactor * scaleFactor)

		fmt.Printf("\n=================================================\n")
		fmt.Printf("🏆 [MPC NODE %d REPORTED FINAL RESULTS]\n", payload.NodeID)
		fmt.Printf("📊 Mean Consumption: %.2f W\n", realMean)
		fmt.Printf("📉 Variance:         %.2f\n", realVariance)
		fmt.Printf("=================================================\n\n")

		w.WriteHeader(http.StatusOK)
	}
}

func stringToUint64(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}
