package lower

import (
	"github.com/tycheg/reasonr-compiler/internal/ir"
	"github.com/tycheg/reasonr-compiler/internal/sema"
)

// uiFuncs lowers layout expressions (RFC §22):
// if conditions, expressions in binding arguments and primitive parameters.
// A function takes the values of the scopes the expression reads
// (sema.Expr.Vars) and returns its value.
func (l *lowerer) uiFuncs() {
	u := l.info.UI
	if u == nil {
		return
	}
	for _, a := range u.Apps {
		l.uiNodes(a.Nodes)
	}
	for _, s := range u.Screens {
		l.uiNodes(s.Nodes)
	}
}

func (l *lowerer) uiNodes(nodes []*sema.Node) {
	for _, n := range nodes {
		l.uiExpr(n.Cond)
		for _, a := range append(append(append([]*sema.Arg(nil), n.Args...), n.ToArgs...), n.Target) {
			if a != nil && a.Source == sema.SrcExpr {
				l.uiExpr(a.Code)
			}
		}
		for _, pv := range n.Params {
			l.uiExpr(pv.Value)
		}
		l.uiNodes(n.Children)
	}
}

func (l *lowerer) uiExpr(e *sema.Expr) {
	if e == nil || e.Func == "" {
		return
	}
	f := l.newFn(ir.FuncUI, e.Func, nil, e.Vars)
	f.f.Result = irType(e.Type)
	f.prologue(e.X)
	f.ret(f.expr(e.X))
	l.finish(f)
}
