package main

import (
	"fmt"
	"log"

	"github.com/msc-privacy-grid-mpc-zkp/cloud-aggregator/internal/api"
	"github.com/msc-privacy-grid-mpc-zkp/cloud-aggregator/internal/config"
	"github.com/msc-privacy-grid-mpc-zkp/cloud-aggregator/internal/zkp"
)

func main() {
	// 1. Load configuration from the isolated config package
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("[FATAL] Error loading configuration: %v", err)
	}

	fmt.Printf("☁️  Starting MPC Cloud Aggregator: [%s]\n", cfg.Server.Name)
	fmt.Printf("---------------------------------------------------------\n")

	// 2. Load the ZKP key
	verifyingKey, err := zkp.LoadVerifyingKey(cfg.ZKP.KeyPath)
	if err != nil {
		log.Fatalf("[FATAL] Failed to load verifying key: %v", err)
	}
	fmt.Println("[SECURITY] ZKP Verifying Key loaded successfully!")

	// 3. Initialize memory store and start server
	store := api.NewMemoryStore(
		cfg.Aggregator.ExpectedMeters,
		cfg.Aggregator.NodeID,
		cfg.Aggregator.OutputPath,
	)
	address := ":" + cfg.Server.Port
	api.StartServer(address, verifyingKey, store, cfg)
}
