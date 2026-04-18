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
	api.ToBinary(circuit.MaxLimit, 32)
	api.AssertIsLessOrEqual(circuit.Consumption, circuit.MaxLimit)

	return nil
}
