package api

import (
	"fmt"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/msc-privacy-grid-mpc-zkp/cloud-aggregator/internal/config"
	"log"
	"net/http"
)

func StartServer(port string, vk groth16.VerifyingKey, store *MemoryStore, cfg *config.AppConfig) {

	// Ruta za dokaze
	http.HandleFunc("/api/proofs", HandleProof(vk, store, cfg.ZKP.MaxLimit))

	// NOVA RUTA sa prosleđenim scale faktorom
	http.HandleFunc("/api/results", HandleMPCResults(cfg.Aggregator.ScaleFactor))

	fmt.Printf("[SERVER] Listening on http://localhost%s\n", port)

	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("[FATAL] Server crashed: %v", err)
	}
}
