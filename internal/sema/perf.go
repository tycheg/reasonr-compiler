package sema

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// Performance tests (*.perf.rr, RFC §27.6): when is a user path of
// several steps, then holds boolean expressions over stats (the run
// statistics) and baseline (the statistics of the baseline run).

// PerfStats is the built-in value object of stats and baseline: call
// latencies, throughput, the number of calls, failures and business errors.
var PerfStats = func() *types.Decl {
	d := &types.Decl{Kind: types.ValueDecl, Name: "Stats", Short: "Stats"}
	for _, f := range []struct {
		name string
		t    types.Type
	}{
		{"p50", tDuration}, {"p90", tDuration}, {"p95", tDuration}, {"p99", tDuration}, {"max", tDuration},
		{"rate", tFloat}, {"calls", tInt}, {"errors", tInt}, {"rejected", tInt},
	} {
		d.AddField(&types.Var{Name: f.name, Type: f.t})
	}
	return d
}()

// PerfMethod is a method in stats.of(Type.method): the full name "Ordering.Customer.OrderTaxi".
type PerfMethod struct{ Name string }

var textPerfThen = "a performance test then holds boolean expressions over stats and baseline: stats.p95 < 100ms"

// perfThen checks a then check of a performance test.
func (b *body) perfThen(it ast.ThenItem) {
	t, ok := it.(*ast.ThenExpr)
	if !ok {
		b.c.bag.Add(diag.E0804, rangeOf(it), diag.Message{Format: textPerfThen})
		return
	}
	b.perf = true
	defer func() { b.perf = false }()
	b.check(t.X, tBool)
}

// perfOf checks stats.of(Type.method): the method is a public command or query.
func (b *body) perfOf(x *ast.CallExpr) types.Type {
	if len(x.Args.Args) != 1 || x.Args.Args[0].Name != nil {
		b.c.bag.Add(diag.E0214, rangeOf(x), diag.Message{Format: textBuiltinArgs, Args: []any{"stats.of", "a method: Type.method"}})
		return perfStatsType
	}
	arg := x.Args.Args[0].Value
	sel, ok := arg.(*ast.SelectorExpr)
	var d *types.Decl
	if ok {
		if p := path(sel); len(p) > 1 {
			d, _ = b.c.lookupPrefix(sel.X, names(p[:len(p)-1]))
		}
	}
	var m *types.Method
	if d != nil {
		m = d.Method(sel.Sel.Name)
	}
	if m == nil || (m.Kind != types.Command && m.Kind != types.Query) || !m.Public() {
		b.c.bag.Add(diag.E0221, rangeOf(arg), "stats.of", "public command or query "+b.c.sourceText(arg))
		return perfStatsType
	}
	b.c.info.PerfMethods[x] = d.Name + "." + m.Name
	return perfStatsType
}

// perfStatsType is the type of stats and baseline.
var perfStatsType = &types.Value{Decl: PerfStats}

// IsPerfStats reports whether t is stats or baseline of a performance test.
func IsPerfStats(t types.Type) bool {
	v, ok := t.(*types.Value)
	return ok && v.Decl == PerfStats
}
