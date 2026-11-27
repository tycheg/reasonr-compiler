package pipeline

import (
	"github.com/tycheg/reasonr-compiler/internal/codegen"
	"github.com/tycheg/reasonr-compiler/internal/ir"
	"github.com/tycheg/reasonr-compiler/internal/lower"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
	"github.com/tycheg/reasonr-compiler/internal/schemagen"
)

// Build builds the program of a model without errors: IR, schema and
// bytecode. The VM (runtime/vm) runs the program; the engine loads its
// schema and bytecode. error is a compiler failure.
func Build(m *Model) (*bytecode.Program, error) {
	return BuildIR(m, lower.Program(m.Program, m.Info, m.Checks))
}

// BuildIR builds the bytecode of model m from its IR prog (possibly
// modified: the mutation check of test impact analysis).
func BuildIR(m *Model, prog *ir.Program) (*bytecode.Program, error) {
	if err := ir.Verify(prog); err != nil {
		return nil, err
	}
	bc, err := codegen.Generate(prog, schemagen.Generate(m.Program, m.Info))
	if err != nil {
		return nil, err
	}
	return bc, bytecode.Verify(bc)
}
