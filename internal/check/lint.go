package check

import (
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// Lint reports warnings about likely mistakes (plan, §12.8).
var Lint = &Analyzer{
	Name:  "lint",
	Doc:   "unused declarations, infinite loops, commands in loops, float for money, notifications from timers",
	Codes: []diag.Code{"W1001", "W1003", "W1004", "W1005", "W1006"},
	Run:   runLint,
}

var unusedKinds = map[types.MethodKind]string{
	types.Command: "Private command",
	types.Event:   "Event",
	types.Error:   "Error",
	types.Rule:    "Rule",
}

func runLint(pass *Pass) any {
	info := pass.Info
	used := map[*types.Method]bool{}
	direct := map[*types.Method]bool{} // commands called not only by a timer
	for _, c := range info.Calls {
		if c.Method != nil {
			used[c.Method] = true
		}
	}
	for _, m := range bodies(info) {
		for _, call := range calls(info, m.body) {
			if c := info.Calls[call]; c.Method != nil {
				direct[c.Method] = true
			}
		}
	}
	if info.UI != nil {
		for _, m := range info.UI.Methods() {
			used[m] = true
			direct[m] = true
		}
	}
	usedFields := map[*types.Var]bool{}
	for _, o := range info.Uses {
		if o.Kind == sema.ObjField && o.Var != nil {
			usedFields[o.Var] = true
		}
	}
	for _, d := range info.DeclList {
		for _, ix := range d.Indexes {
			if ix.Node.Error != nil {
				if m := d.Method(ix.Node.Error.Name); m != nil {
					used[m] = true
				}
			}
			for _, k := range ix.Node.Keys {
				if f := d.Field(k.Path.Elems[0].Name); f != nil {
					usedFields[f] = true
				}
			}
		}
	}

	for _, d := range info.DeclList {
		if !d.Kind.HasID() {
			continue
		}
		for _, f := range d.Fields {
			if !usedFields[f] {
				pass.Report(diag.W1001, rangeOf(f.Node.(*ast.FieldDecl).Name), "Field", f.Name)
			}
		}
		for _, m := range d.Methods {
			kind, ok := unusedKinds[m.Kind]
			if !ok || used[m] || m.Kind == types.Command && m.Public() {
				continue
			}
			pass.Report(diag.W1001, rangeOf(memberIdent(m)), kind, m.Name)
		}
	}
	for _, d := range info.DeclList {
		for _, f := range d.Fields {
			money(pass, f, d)
		}
		for _, m := range d.Methods {
			for _, prm := range m.Params {
				money(pass, prm, d)
			}
		}
	}
	for _, m := range bodies(info) {
		loops(pass, m)
		if cmd, ok := m.node.(*ast.CommandDecl); ok {
			md := m.decl.Method(cmd.Name.Name)
			if md != nil && md.Node == cmd && !md.Public() && used[md] && !direct[md] {
				timerNotifications(pass, m, md)
			}
		}
	}
	return nil
}

func money(pass *Pass, v *types.Var, d *types.Decl) {
	if !isFloat(v.Type) {
		return
	}
	name := strings.ToLower(v.Name)
	if d.Name == "Money" || strings.Contains(name, "price") || strings.Contains(name, "amount") || strings.Contains(name, "sum") {
		var id *ast.Ident
		switch n := v.Node.(type) {
		case *ast.FieldDecl:
			id = n.Name
		case *ast.Param:
			id = n.Name
		default:
			return
		}
		pass.Report(diag.W1005, rangeOf(id), v.Name)
	}
}

func isFloat(t types.Type) bool {
	b, ok := types.Unwrap(t).(*types.Basic)
	return ok && b.Kind == types.Float
}

// loops reports while without an exit (W1003) and commands in a loop over a collection (W1004).
func loops(pass *Pass, m member) {
	info := pass.Info
	ast.Inspect(m.body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.WhileStmt:
			if !hasExit(s.Body) && !assignsAny(info, s.Body, s.Cond) {
				pass.Report(diag.W1003, rangeOf(s.Cond))
			}
		case *ast.ForStmt:
			if _, literal := s.X.(*ast.BracketExpr); literal {
				return true
			}
			for _, call := range calls(info, s.Body) {
				if c := info.Calls[call]; c.Kind == sema.CallCommand {
					pass.Report(diag.W1004, rangeOf(call), exprText(pass, s.X))
					break
				}
			}
		}
		return true
	})
}

func hasExit(b *ast.Block) bool {
	found := false
	ast.Inspect(b, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.ReturnStmt:
			found = true
		case *ast.BranchStmt:
			found = found || s.Tok.String() == "break"
		case *ast.ForStmt, *ast.WhileStmt:
			return false // break of a nested loop does not exit this one
		}
		return !found
	})
	return found
}

// assignsAny reports whether the body assigns a variable of the condition.
func assignsAny(info *sema.Info, body *ast.Block, cond ast.Expr) bool {
	vars := map[*sema.Object]bool{}
	ast.Inspect(cond, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if o := info.Uses[id]; o != nil {
				vars[o] = true
			}
		}
		return true
	})
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if s, ok := n.(*ast.AssignStmt); ok {
			if id, ok := s.Target.(*ast.Ident); ok {
				if o := info.Uses[id]; o != nil && vars[o] {
					found = true
				}
			}
		}
		return !found
	})
	return found
}

func exprText(pass *Pass, x ast.Expr) string {
	f := pass.File(x.Pos())
	if f == nil {
		return "…"
	}
	return string(f.Source()[f.Offset(x.Pos()):f.Offset(x.End())])
}

// timerNotifications reports notifications without to in a command that only a timer calls (W1006).
func timerNotifications(pass *Pass, m member, md *types.Method) {
	ast.Inspect(m.body, func(n ast.Node) bool {
		s, ok := n.(*ast.ExprStmt)
		if !ok || s.To != nil {
			return true
		}
		if call, ok := s.X.(*ast.CallExpr); ok {
			if c := pass.Info.Calls[call]; c != nil && c.Method != nil && c.Method.Kind == types.Notification {
				pass.Report(diag.W1006, rangeOf(call), m.decl.Name+"."+md.Name)
			}
		}
		return true
	})
}
