package main

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"io/ioutil"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/msc-privacy-grid-mpc-zkp/mpc-aggregator/internal/api"
	"github.com/msc-privacy-grid-mpc-zkp/mpc-aggregator/internal/config"
	"github.com/msc-privacy-grid-mpc-zkp/mpc-aggregator/internal/zkp"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("[FATAL] Error loading configuration: %v", err)
	}

	log.Printf("[INFO] Starting MPC Aggregator: %s", cfg.Server.Name)
	log.Println("---------------------------------------------------------")

	verifyingKey, err := zkp.LoadVerifyingKey(cfg.ZKP.KeyPath)
	if err != nil {
		log.Fatalf("[FATAL] Failed to load verifying key: %v", err)
	}
	log.Printf("[SECURITY] ZKP Verifying Key loaded successfully!")
	// Validate configured client CNs have corresponding cert files in secrets
	validateClientCerts(cfg)

	rootCtx := context.Background()
	ctx, stop := signal.NotifyContext(rootCtx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	store := api.NewMemoryStore(ctx,
		cfg.Aggregator.ExpectedMeters,
		cfg.Aggregator.NodeID,
		cfg.Aggregator.OutputPath,
	)

	mux := api.RegisterHandlers(verifyingKey, store, cfg)

	// Start server (mTLS if enabled, otherwise HTTP)
	server, err := api.StartServer(ctx, mux, cfg)
	if err != nil {
		log.Fatalf("[FATAL] Failed to start server: %v", err)
	}

	// On shutdown, gracefully stop the server
	go func() {
		<-ctx.Done()
		log.Println("[SHUTDOWN] Signal received, shutting down server gracefully...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if server != nil {
			if err := server.Shutdown(shutdownCtx); err != nil {
				log.Printf("[ERROR] Server graceful shutdown failed: %v", err)
			}
		}
		log.Println("[SHUTDOWN] Server stopped")
	}()

	// Block until context cancelled
	<-ctx.Done()

	// main exits after shutdown goroutine completes
	log.Println("[EXIT] main exiting")

	// Block until context cancelled
	<-ctx.Done()

	// main exits after shutdown goroutine completes
	log.Println("[EXIT] main exiting")
}

// validateClientCerts scans known secrets locations for certificate files and ensures
// each CN configured in cfg.ClientCNToNode is present. Logs warnings for missing CNs.
func validateClientCerts(cfg *config.AppConfig) {
	if cfg == nil || cfg.ClientCNToNode == nil || len(cfg.ClientCNToNode) == 0 {
		log.Println("[INFO] No client CN->NodeID mapping configured; skipping cert presence validation")
		return
	}

	paths := []string{"/run/secrets", "./secrets"}
	found := make(map[string]bool)

	for _, base := range paths {
		st, err := os.Stat(base)
		if err != nil {
			continue
		}
		if st.IsDir() {
			files, err := ioutil.ReadDir(base)
			if err != nil {
				continue
			}
			for _, f := range files {
				if f.IsDir() {
					continue
				}
				p := filepath.Join(base, f.Name())
				b, err := ioutil.ReadFile(p)
				if err != nil {
					continue
				}
				for {
					var block *pem.Block
					block, b = pem.Decode(b)
					if block == nil {
						break
					}
					if block.Type == "CERTIFICATE" {
						cert, err := x509.ParseCertificate(block.Bytes)
						if err == nil {
							cn := strings.TrimSpace(cert.Subject.CommonName)
							if cn != "" {
								found[cn] = true
							}
						}
					}
				}
			}
		} else {
			// base is a file -> try parse it
			b, err := ioutil.ReadFile(base)
			if err != nil {
				continue
			}
			for {
				var block *pem.Block
				block, b = pem.Decode(b)
				if block == nil {
					break
				}
				if block.Type == "CERTIFICATE" {
					cert, err := x509.ParseCertificate(block.Bytes)
					if err == nil {
						cn := strings.TrimSpace(cert.Subject.CommonName)
						if cn != "" {
							found[cn] = true
						}
					}
				}
			}
		}
	}

	for cn := range cfg.ClientCNToNode {
		if !found[cn] {
			log.Printf("[WARNING] Configured client CN '%s' not found in secrets directories", cn)
		} else {
			log.Printf("[INFO] Found client certificate for CN '%s'", cn)
		}
	}
}
