package zkp

import (
	"bytes"
	"fmt"
	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
	"os"
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

func LoadVerifyingKey(filepath string) (groth16.VerifyingKey, error) {
	vk := groth16.NewVerifyingKey(ecc.BN254)

	f, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open vk file: %w", err)
	}
	defer f.Close()

	_, err = vk.ReadFrom(f)
	if err != nil {
		return nil, fmt.Errorf("failed to read vk data: %w", err)
	}

	return vk, nil
}
