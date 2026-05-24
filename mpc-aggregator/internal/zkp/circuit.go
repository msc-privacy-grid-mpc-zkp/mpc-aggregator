package zkp

import (
	"github.com/consensys/gnark/frontend"
	"github.com/consensys/gnark/std/hash/mimc"
)

// RangeProofCircuit defines the mathematical constraints for the zero-knowledge proof,
// ensuring the consumption is within limits and cryptographically bound to the meter.
type RangeProofCircuit struct {
	Consumption frontend.Variable
	MaxLimit    frontend.Variable `gnark:",public"`
	MeterID     frontend.Variable `gnark:",public"`
	Timestamp   frontend.Variable `gnark:",public"`
	Commitment  frontend.Variable `gnark:",public"`
}

// Define declares the circuit's constraints to the gnark compiler, including
// the range check and the cryptographic binding of the public parameters.
func (circuit *RangeProofCircuit) Define(api frontend.API) error {
	api.ToBinary(circuit.MaxLimit, 32)
	api.AssertIsLessOrEqual(circuit.Consumption, circuit.MaxLimit)

	// Kreiraj MiMC za krug
	h, err := mimc.NewMiMC(api)
	if err != nil {
		return err
	}

	// Prosledi sve varijable odjednom (gnark će ih tretirati kao elemente polja)
	h.Write(circuit.Consumption, circuit.MeterID, circuit.Timestamp)
	hash := h.Sum()

	api.AssertIsEqual(hash, circuit.Commitment)

	return nil
}
