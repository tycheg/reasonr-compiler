package check

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// Unique checks the errors of unique indexes (plan, §12.3).
var Unique = &Analyzer{
	Name:  "unique",
	Doc:   "the unique error is declared, its parameters are the last segments of the index paths",
	Codes: []diag.Code{"E0511", "E0512"},
	Run:   runUnique,
}

func runUnique(pass *Pass) any {
	for _, d := range pass.Info.DeclList {
		for _, ix := range d.Indexes {
			n := ix.Node
			if n.Error == nil {
				continue
			}
			m := d.Method(n.Error.Name)
			if m == nil || m.Kind != types.Error {
				pass.Report(diag.E0511, rangeOf(n.Error), n.Error.Name, d.Name)
				continue
			}
			// The last path segment → the field type.
			last := map[string]types.Type{}
			for _, k := range n.Keys {
				if t := pathType(d, k.Path); t != nil {
					last[k.Path.Elems[len(k.Path.Elems)-1].Name] = t
				}
			}
			for _, p := range m.Params {
				t, ok := last[p.Name]
				if !ok || !sameType(t, p.Type) {
					pass.Report(diag.E0512, rangeOf(p.Node.(*ast.Param).Name), p.Name)
				}
			}
		}
	}
	return nil
}

// pathType returns the type of a field by a path through value objects, or nil.
func pathType(d *types.Decl, path *ast.Path) types.Type {
	var t types.Type
	owner := d
	for i, e := range path.Elems {
		if owner == nil {
			return nil
		}
		f := owner.Field(e.Name)
		if f == nil {
			return nil
		}
		t = f.Type
		if i < len(path.Elems)-1 {
			v, ok := types.Unwrap(t).(*types.Value)
			if !ok {
				return nil
			}
			owner = v.Decl
		}
	}
	return t
}

// sameType compares types ignoring "?": an index field may be optional,
// while an error parameter may not.
func sameType(a, b types.Type) bool {
	return types.Unwrap(a).String() == types.Unwrap(b).String()
}

// Access checks access lists (plan, §12.4).
var Access = &Analyzer{
	Name:  "access",
	Doc:   "only public methods have access; a public command without access is a warning",
	Codes: []diag.Code{"E0605", "W1010"},
	Run:   runAccess,
}

func runAccess(pass *Pass) any {
	for _, d := range pass.Info.DeclList {
		for _, m := range d.Methods {
			var access *ast.AccessList
			switch n := m.Node.(type) {
			case *ast.CommandDecl:
				access = n.Access
			case *ast.QueryDecl:
				access = n.Access
			default:
				continue
			}
			switch {
			case access != nil && !m.Public():
				pass.Report(diag.E0605, rangeOf(access), d.Name+"."+m.Name)
			case access == nil && m.Public() && m.Kind == types.Command && d.Kind != types.ExternalDecl:
				// Only a test calls fake commands.
				pass.Report(diag.W1010, rangeOf(memberIdent(m)), d.Name+"."+m.Name)
			}
		}
	}
	return nil
}

func memberIdent(m *types.Method) *ast.Ident {
	switch n := m.Node.(type) {
	case *ast.CommandDecl:
		return n.Name
	case *ast.QueryDecl:
		return n.Name
	case *ast.EventDecl:
		return n.Name
	case *ast.ErrorDecl:
		return n.Name
	case *ast.NotificationDecl:
		return n.Name
	case *ast.RuleDecl:
		return n.Name
	case *ast.MethodDecl:
		return n.Name
	case *ast.RequestDecl:
		return n.Name
	}
	return nil
}
