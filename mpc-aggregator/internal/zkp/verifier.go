package zkp

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
)

var proofPool = sync.Pool{
	New: func() interface{} {
		return groth16.NewProof(ecc.BN254)
	},
}

// VerifyProof verifies a ZKP proof with cryptographic binding and freshness check.
// The proof is bound to specific MeterID and Timestamp values through the circuit constraints.
// Additionally, a freshness check ensures the timestamp is within a valid time window.
func VerifyProof(proofBytes []byte, maxLimit, meterID, timestamp uint64, verifyingKey groth16.VerifyingKey) error {
	// Freshness check: Ensure timestamp is within +/- 60 seconds of current server time
	// This prevents replay attacks by rejecting proofs with stale timestamps
	currentTime := time.Now().Unix()
	timestampInt64 := int64(timestamp)
	timeDiff := currentTime - timestampInt64

	const FRESHNESS_WINDOW = int64(60) // seconds

	if timeDiff > FRESHNESS_WINDOW || timeDiff < -FRESHNESS_WINDOW {
		return fmt.Errorf("proof timestamp is stale or in the future: current=%d, proof=%d, diff=%d seconds (max allowed: %d)", 
			currentTime, timestampInt64, timeDiff, FRESHNESS_WINDOW)
	}

	proof := proofPool.Get().(groth16.Proof)
	defer proofPool.Put(proof)

	_, err := proof.ReadFrom(bytes.NewReader(proofBytes))
	if err != nil {
		return fmt.Errorf("failed to deserialize proof from bytes: %w", err)
	}

	// Create public witness with the exact public inputs used during proof generation.
	// The circuit enforces:
	// 1. Range check: Consumption <= MaxLimit
	// 2. Cryptographic binding: (Consumption + MeterID) * (1 + Timestamp)
	//
	// If any public input (MaxLimit, MeterID, or Timestamp) differs from what was
	// used during proof generation, the verification will fail.
	assignment := &RangeProofCircuit{
		MaxLimit:  maxLimit,
		MeterID:   meterID,
		Timestamp: timestamp,
	}

	publicWitness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
	if err != nil {
		return fmt.Errorf("failed to create public witness: %w", err)
	}

	err = groth16.Verify(proof, verifyingKey, publicWitness)
	if err != nil {
		return fmt.Errorf("cryptographic verification failed: %w", err)
	}

	return nil
}

func LoadVerifyingKey(filepath string) (groth16.VerifyingKey, error) {
	verifyingKey := groth16.NewVerifyingKey(ecc.BN254)

	f, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open verifying key file: %w", err)
	}

	defer func() {
		if closeErr := f.Close(); closeErr != nil {
			log.Printf("[WARNING] Failed to close verifying key file '%s': %v", filepath, closeErr)
		}
	}()

	_, err = verifyingKey.ReadFrom(f)
	if err != nil {
		return nil, fmt.Errorf("failed to read verifying key data: %w", err)
	}

	return verifyingKey, nil
}
