package vm

import (
	"context"
	"fmt"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
)

// Eval evaluates layout expression name, a ui function
// (RFC §22): an if condition or an expression in
// an argument. args are the scope values in arena a in function parameter
// order; the result is copied into the same arena. Expressions are pure:
// they do not read the store, so no Env is needed; me comes from caller.
func (m *Machine) Eval(ctx context.Context, name string, args []model.Value, a *model.Arena, caller model.Caller, now time.Time) (v model.Value, err error) {
	defer func() {
		if p := recover(); p != nil {
			v, err = model.None, caught(p)
		}
	}()
	fi := m.prog.FuncIndex(bytecode.FuncUI, name)
	if fi == bytecode.NoFunc {
		return model.None, &Fault{Err: fmt.Errorf("%w: layout expression %q", ErrNotFound, name)}
	}
	r := m.start(ctx, nil, caller, a, now)
	defer func() {
		model.PutArena(r.a)
		r.finish()
	}()
	in := make([]model.Value, len(args))
	for i, x := range args {
		in[i] = r.importValue(a, x)
	}
	out, err := r.call(fi, in, frame{})
	if err != nil {
		return model.None, err
	}
	return a.Copy(r.a, out), nil
}
