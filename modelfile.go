package reasonr

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"os"

	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
	"github.com/tycheg/reasonr-compiler/internal/runtime/vm"
)

// A model file (reasonr build -o) holds a built model: the program, the
// interface program and what the engine asks about the sources (the
// scenarios, the performance checks, the files and their hash). The format
// is the compiler's own: only Encode writes it and only Open
// read it, of the same compiler version.

// fileMagic starts a model file; the digit is the format version.
const fileMagic = "reasonr-model-1\n"

// modelFile is the content of a model file.
type modelFile struct {
	Extension string
	Test      bool
	Program   []byte // bytecode.Encode
	UI        *UI
	Scenarios []Scenario
	Perf      map[string][]string
	Files     map[string]string
	Hash      string
}

// Encode returns the model file of the model.
func (m *Model) Encode() ([]byte, error) {
	prog, err := bytecode.Encode(m.bc)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.WriteString(fileMagic)
	err = gob.NewEncoder(&b).Encode(&modelFile{Extension: m.ext, Test: m.test, Program: prog, UI: m.UI(),
		Scenarios: m.scenarios, Perf: m.perf, Files: m.files, Hash: m.hash})
	return b.Bytes(), err
}

// decode reads a model from a model file's content.
func decode(data []byte) (*Model, error) {
	rest, ok := bytes.CutPrefix(data, []byte(fileMagic))
	if !ok {
		return nil, errors.New("not a model file of this compiler version (reasonr build -o)")
	}
	var f modelFile
	if err := gob.NewDecoder(bytes.NewReader(rest)).Decode(&f); err != nil {
		return nil, fmt.Errorf("model file: %w", err)
	}
	bc, err := bytecode.Decode(f.Program)
	if err != nil {
		return nil, fmt.Errorf("model file: %w", err)
	}
	mach, err := vm.New(bc, vm.DefaultLimits)
	if err != nil {
		return nil, fmt.Errorf("model file: %w", err)
	}
	m := &Model{bc: bc, mach: mach, ext: f.Extension, test: f.Test, ui: f.UI,
		scenarios: f.Scenarios, perf: f.Perf, files: f.Files, hash: f.Hash}
	if m.ui == nil {
		m.ui = &UI{}
	}
	return m, nil
}

// Open reads the model file at path (reasonr build -o).
func Open(path string) (*Model, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	m, err := decode(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return m, nil
}
