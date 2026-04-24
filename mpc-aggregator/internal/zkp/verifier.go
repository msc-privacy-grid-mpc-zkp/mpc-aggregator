package zkp

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"sync"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
)

// Креирамо базен (Pool) који рециклира Proof објекте.
// Ово спречава Go Garbage Collector да се "гуши" приликом 10.000+ захтева.
var proofPool = sync.Pool{
	New: func() interface{} {
		return groth16.NewProof(ecc.BN254)
	},
}

func VerifyProof(proofBytes []byte, maxLimit, meterID, timestamp uint64, verifyingKey groth16.VerifyingKey) error {
	// 1. Узимамо празан Proof објекат из базена уместо да алоцирамо нови
	proof := proofPool.Get().(groth16.Proof)

	// 2. Враћамо га у базен када функција заврши
	defer proofPool.Put(proof)

	// Десеријализација преписује податке преко постојећег објекта
	_, err := proof.ReadFrom(bytes.NewReader(proofBytes))
	if err != nil {
		return fmt.Errorf("failed to deserialize proof from bytes: %w", err)
	}

	assignment := &RangeProofCircuit{
		MaxLimit:  maxLimit,
		MeterID:   meterID,
		Timestamp: timestamp,
	}

	publicWitness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
	if err != nil {
		return fmt.Errorf("failed to create public witness: %w", err)
	}

	// Напомена: groth16.Verify је thread-safe за verifyingKey,
	// тако да више HTTP рутина може паралелно звати ову функцију без Mutex-а.
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
			log.Printf("[WARNING] Failed to close verifying key file '%s': %v\n", filepath, closeErr)
		}
	}()

	_, err = verifyingKey.ReadFrom(f)
	if err != nil {
		return nil, fmt.Errorf("failed to read verifying key data: %w", err)
	}

	return verifyingKey, nil
}
