package api

import (
	"net/http"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/msc-privacy-grid-mpc-zkp/mpc-aggregator/internal/config"
)

// RegisterHandlers registers HTTP routes and returns an http.Handler suitable for use in a server
func RegisterHandlers(vk groth16.VerifyingKey, store *MemoryStore, cfg *config.AppConfig) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/proofs", HandleProof(vk, store, cfg.ZKP.MaxLimit))
	mux.HandleFunc("/api/results", HandleMPCResults(cfg.Aggregator.ScaleFactor, cfg))
	return mux
}
