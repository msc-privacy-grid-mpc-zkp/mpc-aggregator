package main

import (
	"fmt"
	"github.com/msc-privacy-grid-mpc-zkp/cloud-aggregator/internal/api"
	"github.com/msc-privacy-grid-mpc-zkp/cloud-aggregator/internal/zkp"
	"log"
)

func main() {
	fmt.Println("☁️  Starting MPC Cloud Aggregator...")

	// 1. Učitavamo ključ sa diska
	// (Pretpostavka je da si 'verifying.key' iskopirao u root projekta ili u folder 'keys')
	vk, err := zkp.LoadVerifyingKey("keys/verifying.key")
	if err != nil {
		log.Fatalf("[FATAL] Failed to load verifying key: %v", err)
	}
	fmt.Println("[SECURITY] ZKP Verifying Key loaded successfully!")

	// 2. Startujemo server i prosleđujemo mu ključ
	store := api.NewMemoryStore(10)
	api.StartServer(":8080", vk, store)
}
