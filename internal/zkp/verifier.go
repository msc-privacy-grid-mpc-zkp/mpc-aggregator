package zkp

import (
	"bytes"
	"fmt"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
)

func VerifyProof(proofBytes []byte, maxLimit uint64, verifyingKey groth16.VerifyingKey) error {
	proof := groth16.NewProof(ecc.BN254)
	_, err := proof.ReadFrom(bytes.NewReader(proofBytes))
	if err != nil {
		return fmt.Errorf("failed to deserialize proof from bytes: %w", err)
	}

	assigment := &RangeProofCircuit{
		MaxLimit: maxLimit,
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
