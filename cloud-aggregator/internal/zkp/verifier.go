package zkp

import (
	"bytes"
	"fmt"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"log"
	"os"
)

func VerifyProof(proofBytes []byte, maxLimit, meterID, timestamp uint64, verifyingKey groth16.VerifyingKey) error {
	proof := groth16.NewProof(ecc.BN254)
	_, err := proof.ReadFrom(bytes.NewReader(proofBytes))
	if err != nil {
		return fmt.Errorf("failed to deserialize proof from bytes: %w", err)
	}

	assigment := &RangeProofCircuit{
		MaxLimit:  maxLimit,
		MeterID:   meterID,
		Timestamp: timestamp,
	}

	publicWitness, err := frontend.NewWitness(assigment, ecc.BN254.ScalarField(), frontend.PublicOnly())
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
			log.Printf("[WARNING] Failed to close verifying key file '%s': %v\n", filepath, closeErr)
		}
	}()

	_, err = verifyingKey.ReadFrom(f)
	if err != nil {
		return nil, fmt.Errorf("failed to read verifying key data: %w", err)
	}

	return verifyingKey, nil
}
