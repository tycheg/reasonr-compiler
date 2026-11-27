package lower

import (
	"fmt"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/ir"
	"github.com/tycheg/reasonr-compiler/internal/sema"
)

// A performance test (RFC §27.6): the test function is given and the
// when path, without then; each then check is a function PerfCheck(test, i)
// with a bool result, in which stats.field and stats.of(Method).field are
// the built-in perf.stat with key PerfKey.

// PerfCheck is the name of check function i of test test; baseline: the
// check reads baseline (it is skipped without a baseline run).
func PerfCheck(test string, i int, baseline bool) string {
	name := fmt.Sprintf("%s\x00then%d", test, i)
	if baseline {
		name += "\x00baseline"
	}
	return name
}

// PerfKey is the perf.stat key: "baseline|Ordering.Customer.OrderTaxi|p99";
// the base and the method may be empty.
func PerfKey(baseline bool, method, field string) string {
	b := ""
	if baseline {
		b = "baseline"
	}
	return b + "|" + method + "|" + field
}

// perfChecks lowers the then checks of a performance test.
func (l *lowerer) perfChecks(d *ast.TestDecl) {
	if d.Then == nil {
		return
	}
	for i, it := range d.Then.Items {
		t, ok := it.(*ast.ThenExpr)
		if !ok {
			continue
		}
		baseline := false
		ast.Inspect(t.X, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && id.Name == "baseline" && l.info.Uses[id] != nil && l.info.Uses[id].Kind == sema.ObjBuiltin {
				baseline = true
			}
			return true
		})
		f := l.newFn(ir.FuncTest, PerfCheck(d.Name.Value, i, baseline), nil, nil)
		f.f.Result = ir.Basic(ir.TBool)
		f.prologue(t.X)
		f.at(t.Pos())
		f.emit(ir.Instr{Op: ir.OpRetVal, Args: []ir.Operand{f.expr(t.X)}})
		l.finish(f)
	}
}

// perfStat lowers stats.field, baseline.field, stats.of(Method).field; ok
// reports that it is one of them.
func (f *fn) perfStat(x *ast.SelectorExpr) (ir.Operand, bool) {
	if !sema.IsPerfStats(f.typeOf(x.X)) {
		return ir.NoOperand, false
	}
	var base, method string
	switch y := x.X.(type) {
	case *ast.Ident:
		base = y.Name
	case *ast.CallExpr:
		if fun, ok := y.Fun.(*ast.SelectorExpr); ok {
			if id, ok := fun.X.(*ast.Ident); ok {
				base = id.Name
			}
		}
		method = f.l.info.PerfMethods[y]
	}
	key := PerfKey(base == "baseline", method, x.Sel.Name)
	return f.value(irType(f.typeOf(x)), ir.Instr{Op: ir.OpIntrinsic, Sym: "perf.stat", Args: []ir.Operand{f.konst(strConst(key))}}), true
}
