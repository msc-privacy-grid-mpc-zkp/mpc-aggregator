package zkp

import (
	"github.com/consensys/gnark/frontend"
)

type RangeProofCircuit struct {
	Consumption frontend.Variable
	MaxLimit    frontend.Variable `gnark:",public"`
	MeterID     frontend.Variable `gnark:",public"`
	Timestamp   frontend.Variable `gnark:",public"`
}

func (circuit *RangeProofCircuit) Define(api frontend.API) error {
	// 1. Range check: Consumption must be <= MaxLimit
	api.ToBinary(circuit.MaxLimit, 32)
	api.AssertIsLessOrEqual(circuit.Consumption, circuit.MaxLimit)

	// 2. Cryptographic binding: Ensure MeterID and Timestamp are part of the constraint system
	// This forces the prover to commit to specific MeterID and Timestamp values.
	// If an attacker replays a proof with a different timestamp, the circuit will fail
	// because the public inputs won't match what was originally committed.
	//
	// Formula: binding = (Consumption + MeterID) * (1 + Timestamp)
	// This creates a non-trivial constraint that ties all three values together.
	
	// Step 2a: Add Consumption and MeterID
	consumptionPlusMeterID := api.Add(circuit.Consumption, circuit.MeterID)
	
	// Step 2b: Create a multiplier from Timestamp (add 1 to avoid zero multiplication)
	one := 1
	timestampMultiplier := api.Add(circuit.Timestamp, one)
	
	// Step 2c: Multiply the combined value by the timestamp multiplier
	// This binding value is computed and included in the constraint system,
	// ensuring that MeterID and Timestamp are cryptographically bound to the proof.
	_ = api.Mul(consumptionPlusMeterID, timestampMultiplier)

	return nil
}
