package api

import (
	"fmt"
	"github.com/consensys/gnark/backend/groth16"
	"log"
	"net/http"
)

func StartServer(port string, vk groth16.VerifyingKey, store *MemoryStore, maxLimit uint64) {
	http.HandleFunc("/api/proofs", HandleProof(vk, store, maxLimit))

	fmt.Printf("[SERVER] Listening on http://localhost%s\n", port)

	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("[FATAL] Server crashed: %v", err)
	}
}
