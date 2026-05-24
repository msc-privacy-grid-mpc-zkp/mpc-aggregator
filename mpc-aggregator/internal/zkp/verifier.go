package zkp

import (
	"bytes"
	"fmt"
	"log"
	"math/big"
	"os"
	"sync"

	"github.com/consensys/gnark-crypto/ecc"
	"github.com/consensys/gnark/backend/groth16"
	"github.com/consensys/gnark/frontend"
)

var proofPool = sync.Pool{
	New: func() interface{} {
		return groth16.NewProof(ecc.BN254)
	},
}

func VerifyProof(proofBytes []byte, maxLimit, meterID, timestamp uint64, commitmentBytes []byte, verifyingKey groth16.VerifyingKey) error {
	// 1. Uvek instanciramo potpuno nov, čist objekat za dokaz
	proof := groth16.NewProof(ecc.BN254)

	// 2. Čitamo bajtove sa mreže
	if _, err := proof.ReadFrom(bytes.NewReader(proofBytes)); err != nil {
		return fmt.Errorf("failed to deserialize proof from bytes: %w", err)
	}

	// 3. Pripremamo javne parametre (uz dodatak praznog tajnog parametra radi sigurnosti kompajlera)
	assignment := &RangeProofCircuit{
		Consumption: 0, // Gnark ovo ignoriše jer nije public, ali sprečava nil pointere u refleksiji
		MaxLimit:    maxLimit,
		MeterID:     meterID,
		Timestamp:   timestamp,
		Commitment:  new(big.Int).SetBytes(commitmentBytes),
	}

	publicWitness, err := frontend.NewWitness(assignment, ecc.BN254.ScalarField(), frontend.PublicOnly())
	if err != nil {
		return fmt.Errorf("failed to create public witness: %w", err)
	}

	// 4. Verifikacija
	if err = groth16.Verify(proof, verifyingKey, publicWitness); err != nil {
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
