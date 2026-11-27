package vm

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
)

// FuzzExec (plan, stage 8): the bytecode of the examples with randomly
// changed code words. A program the verifier accepted runs without panics:
// all public commands and queries with default argument values; errors and
// failures are allowed.

var fuzzFixtures []*fixture

func fuzzFixture(i byte) *fixture {
	if fuzzFixtures == nil {
		t := &testing.T{}
		for _, c := range []struct{ dir, ext string }{
			{example("taxi"), ""}, {example("taxi"), "acme"}, {example("bank"), ""}, {"../../lower/testdata/constructs", ""},
		} {
			fuzzFixtures = append(fuzzFixtures, load(t, c.dir, c.ext))
		}
	}
	return fuzzFixtures[int(i)%len(fuzzFixtures)]
}

// mutate returns a copy of a program whose code words are replaced by the
// data: a function number, an address (2 bytes) and a new word (4 bytes).
func mutate(p *bytecode.Program, data []byte) *bytecode.Program {
	out := *p
	out.Funcs = append([]bytecode.Func(nil), p.Funcs...)
	for ; len(data) >= 7; data = data[7:] {
		f := &out.Funcs[int(data[0])%len(out.Funcs)]
		f.Code = append([]uint32(nil), f.Code...)
		pc := int(binary.LittleEndian.Uint16(data[1:])) % len(f.Code)
		f.Code[pc] = binary.LittleEndian.Uint32(data[3:])
	}
	return &out
}

func execAll(t *testing.T, m *Machine, s *model.Schema) {
	ctx := context.Background()
	env := newEnv(s)
	a := model.NewArena()
	caller := model.Caller{Kind: model.CallerTimer}
	for _, ti := range s.Types {
		for _, mi := range ti.Methods {
			if mi.Kind != model.MethodCommand && mi.Kind != model.MethodQuery {
				continue
			}
			args := make([]model.Value, len(mi.Params))
			for i, p := range mi.Params {
				if p.Type.Kind == model.KindID {
					args[i] = model.ID(p.Type.Ref, model.UUID([16]byte{15: 1}))
				} else {
					args[i], _ = env.codec.Zero(a, p.Type)
				}
			}
			if mi.Kind == model.MethodCommand {
				if ti.ID != model.IDUUID {
					continue
				}
				out, err := m.Execute(ctx, model.CommandCall{Entity: ti.Ref, ID: model.ID(ti.Ref, model.UUID([16]byte{15: 2})),
					Command: mi.Ref, Args: a.Record(0, args...), Arena: a, Caller: caller}, env)
				internal(t, err)
				if err == nil {
					out.Release()
				}
				continue
			}
			out, err := m.Query(ctx, model.QueryCall{Type: ti.Ref, Query: mi.Ref, Args: a.Record(0, args...), Arena: a,
				Caller: model.Caller{Kind: model.CallerUser, Roles: s.Roles}}, env)
			internal(t, err)
			if err == nil {
				out.Release()
			}
		}
	}
}

// internal: a panic inside the VM (ErrInternal) is an interpreter bug.
func internal(t *testing.T, err error) {
	if errors.Is(err, ErrInternal) {
		t.Fatalf("%v", err)
	}
}

func FuzzExec(f *testing.F) {
	f.Add([]byte{0})
	f.Add([]byte{1, 3, 2, 0, 0x01, 0x02, 0x03, 0x00})
	f.Add([]byte{2, 5, 1, 0, 0x2a, 0x01, 0x00, 0x00})
	f.Add([]byte{3, 9, 4, 0, 0x05, 0x00, 0x01, 0x02})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		fx := fuzzFixture(data[0])
		p := mutate(fx.m.prog, data[1:])
		m, err := New(p, DefaultLimits)
		if err != nil {
			return // the verifier rejected the program
		}
		execAll(t, m, p.Schema)
	})
}
