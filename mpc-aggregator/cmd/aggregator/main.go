package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"os/signal"
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
	httpAddress := ":" + cfg.Server.Port
	srvHTTP := &http.Server{
		Addr:         httpAddress,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Setup mTLS listener on a separate port (configurable) if secrets are available
	tlsAddress := ":" + cfg.Server.TLSPort
	certPath := "/run/secrets/tls_server_cert"
	keyPath := "/run/secrets/tls_server_key"
	caPath := "/run/secrets/tls_ca_cert"

	// Helper to resolve a secret path that may be a file, a directory (Docker behavior on some platforms),
	// or exist with a .pem extension.
	resolvePath := func(p string) (string, error) {
		st, err := os.Stat(p)
		if err == nil {
			if st.IsDir() {
				// pick first regular file inside directory
				fns, err := ioutil.ReadDir(p)
				if err != nil {
					return "", err
				}
				for _, f := range fns {
					if !f.IsDir() {
						return p + "/" + f.Name(), nil
					}
				}
				return "", fmt.Errorf("no file found in directory %s", p)
			}
			return p, nil
		}
		// try with .pem suffix
		if _, err2 := os.Stat(p + ".pem"); err2 == nil {
			return p + ".pem", nil
		}
		return "", err
	}

	var srvTLS *http.Server
	if resolvedCert, err := resolvePath(certPath); err == nil {
		resolvedKey, err := resolvePath(keyPath)
		if err != nil {
			log.Fatalf("[FATAL] Failed to resolve TLS key path: %v", err)
		}

		// Load server certificate
		cert, err := tls.LoadX509KeyPair(resolvedCert, resolvedKey)
		if err != nil {
			log.Fatalf("[FATAL] Failed to load server TLS certificate: %v", err)
		}

		// Load CA pool for client certificate verification
		resolvedCA, err := resolvePath(caPath)
		if err != nil {
			log.Fatalf("[FATAL] Failed to resolve CA path: %v", err)
		}
		caCert, err := ioutil.ReadFile(resolvedCA)
		if err != nil {
			log.Fatalf("[FATAL] Failed to read CA certificate: %v", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caCert) {
			log.Fatalf("[FATAL] Failed to append CA certificate to pool")
		}

		tlsCfg := &tls.Config{
			Certificates:             []tls.Certificate{cert},
			ClientCAs:                pool,
			ClientAuth:               tls.RequireAndVerifyClientCert,
			MinVersion:               tls.VersionTLS12,
			PreferServerCipherSuites: true,
		}

		srvTLS = &http.Server{
			Addr:         tlsAddress,
			Handler:      mux,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  60 * time.Second,
			TLSConfig:    tlsCfg,
		}

		go func() {
			log.Printf("[SERVER] Listening (mTLS) on https://localhost%s", tlsAddress)
			if err := srvTLS.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				log.Fatalf("[FATAL] TLS server crashed: %v", err)
			}
		}()
	} else {
		log.Printf("[SERVER] mTLS disabled; TLS secrets not found, running HTTP-only")
	}

	// Always start HTTP server for external (non-mTLS) clients
	go func() {
		log.Printf("[SERVER] Listening on http://localhost%s", httpAddress)
		if err := srvHTTP.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[FATAL] HTTP server crashed: %v", err)
		}
	}()

	// On shutdown, gracefully stop both servers
	go func() {
		<-ctx.Done()
		log.Println("[SHUTDOWN] Signal received, shutting down servers gracefully...")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()

		if err := srvHTTP.Shutdown(shutdownCtx); err != nil {
			log.Printf("[ERROR] HTTP graceful shutdown failed: %v", err)
		}
		if srvTLS != nil {
			if err := srvTLS.Shutdown(shutdownCtx); err != nil {
				log.Printf("[ERROR] TLS graceful shutdown failed: %v", err)
			}
		}
		log.Println("[SHUTDOWN] Servers stopped")
	}()

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
