package api

import (
	"fmt"
	"github.com/consensys/gnark/backend/groth16"
	"log"
	"net/http"
)

func StartServer(port string, vk groth16.VerifyingKey) {
	http.HandleFunc("/api/proofs", HandleProof(vk))

	fmt.Printf("[SERVER] Listening on http://localhost%s\n", port)

	if err := http.ListenAndServe(port, nil); err != nil {
		log.Fatalf("[FATAL] Server crashed: %v", err)
	}
}
