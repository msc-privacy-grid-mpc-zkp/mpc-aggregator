package api

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/consensys/gnark/backend/groth16"
	"github.com/msc-privacy-grid-mpc-zkp/mpc-aggregator/internal/config"
)

// StartServer starts either an mTLS-enabled HTTPS server or an HTTP server based on cfg.Server.EnableMTLS.
// It returns the started *http.Server so the caller can perform graceful shutdown.
func StartServer(ctx context.Context, handler http.Handler, cfg *config.AppConfig) (*http.Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("nil config provided")
	}

	// Common server timeouts
	readTimeout := 15 * time.Second
	writeTimeout := 30 * time.Second
	idleTimeout := 60 * time.Second

	if cfg.Server.EnableMTLS {
		// Validate paths
		if cfg.Server.ServerCertPath == "" || cfg.Server.ServerKeyPath == "" || cfg.Server.CACertPath == "" {
			return nil, fmt.Errorf("mTLS enabled but cert/key/ca paths are not configured")
		}

		// Load server certificate/key pair
		cert, err := tls.LoadX509KeyPair(cfg.Server.ServerCertPath, cfg.Server.ServerKeyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load server certificate/key: %w", err)
		}

		// Read CA certificate bytes (use os.ReadFile as requested)
		caBytes, err := os.ReadFile(cfg.Server.CACertPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate from %s: %w", cfg.Server.CACertPath, err)
		}

		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caBytes) {
			return nil, fmt.Errorf("failed to append CA certificate to pool from %s", cfg.Server.CACertPath)
		}

		tlsCfg := &tls.Config{
			Certificates:             []tls.Certificate{cert},
			ClientCAs:                pool,
			ClientAuth:               tls.RequireAndVerifyClientCert,
			MinVersion:               tls.VersionTLS12,
			PreferServerCipherSuites: true,
		}

		addr := ":" + cfg.Server.TLSPort
		srv := &http.Server{
			Addr:         addr,
			Handler:      handler,
			ReadTimeout:  readTimeout,
			WriteTimeout: writeTimeout,
			IdleTimeout:  idleTimeout,
			TLSConfig:    tlsCfg,
		}

		// Start in background
		go func() {
			log.Printf("[SERVER] Listening (mTLS) on https://localhost%s", addr)
			if err := srv.ListenAndServeTLS("", ""); err != nil && err != http.ErrServerClosed {
				log.Fatalf("[FATAL] mTLS server crashed: %v", err)
			}
		}()

		return srv, nil
	}

	// Fallback to plain HTTP
	addr := ":" + cfg.Server.Port
	srv := &http.Server{
		Addr:         addr,
		Handler:      handler,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}

	go func() {
		log.Printf("[SERVER] Listening on http://localhost%s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("[FATAL] HTTP server crashed: %v", err)
		}
	}()

	return srv, nil
}

// RegisterHandlers registers HTTP routes and returns an http.Handler suitable for use in a server
func RegisterHandlers(vk groth16.VerifyingKey, store *MemoryStore, cfg *config.AppConfig) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/proofs", HandleProof(vk, store, cfg.ZKP.MaxLimit))
	mux.HandleFunc("/api/results", HandleMPCResults(cfg.Aggregator.ScaleFactor, cfg))
	return mux
}
