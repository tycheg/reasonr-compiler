package lsp

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// UI (RFC §22): names in layouts, default and texts.

// uiNodeOf returns the resolved layout line that contains the path node i
// levels up, or nil.
func (r *resolver) uiNode(path []ast.Node) *sema.Node {
	if r.info.UI == nil {
		return nil
	}
	for i := len(path) - 1; i >= 0; i-- {
		if n, ok := path[i].(ast.UINode); ok {
			return r.info.UI.Nodes[n]
		}
	}
	return nil
}

// uiTarget resolves a name in a layout line, default or texts: a primitive,
// a part, a screen, a binding type and method, a field; ok == false means
// the name is not from the UI.
func (r *resolver) uiTarget(path []ast.Node, id *ast.Ident) (*target, bool) {
	u := r.info.UI
	if u == nil {
		return nil, false
	}
	switch p := parent(path, 1).(type) {
	case *ast.PrimNode:
		if p.Name != id {
			return nil, false
		}
		n := u.Nodes[p]
		switch {
		case n == nil:
		case n.Kind == sema.NodePart:
			return nil, true
		case n.Prim != nil:
			return r.decl(n.Prim.Decl), true
		case n.To != nil:
			return r.decl(n.To.Decl), true
		}
		return nil, true
	case *ast.Binding:
		n := r.uiNode(path)
		switch {
		case n == nil:
			return nil, true
		case p.Method == id && n.Method != nil:
			return r.method(n.Owner, n.Method), true
		case p.Type == id && n.Owner != nil:
			return r.decl(n.Owner), true
		case p.Type == id && n.To != nil:
			return r.decl(n.To.Decl), true
		}
		return nil, true
	case *ast.FieldNode:
		n := u.Nodes[p]
		switch {
		case n == nil:
		case p.Prim == id && n.Prim != nil:
			return r.decl(n.Prim.Decl), true
		case p.Name == id:
			return r.uiField(n), true
		}
		return nil, true
	case *ast.OnNode:
		if n := u.Nodes[p]; n != nil && p.Screen == id && n.To != nil {
			return r.decl(n.To.Decl), true
		}
		return nil, true
	case *ast.DefaultDecl:
		if p.Prim == id {
			if pr := u.Primitive(id.Name); pr != nil {
				return r.decl(pr.Decl), true
			}
		}
		return nil, true
	}
	return nil, false
}

// uiField is a line field: a value field, a method or screen parameter.
func (r *resolver) uiField(n *sema.Node) *target {
	owner := n.Parent
	for owner != nil && owner.Method == nil && owner.Kind == sema.NodePart {
		owner = owner.Parent
	}
	if owner == nil {
		return nil
	}
	if owner.Method != nil {
		for _, a := range owner.Args {
			if a.Name == n.Field && a.Input {
				return r.paramByName(owner.Method, n.Field)
			}
		}
	}
	// A data field: look for the field declaration in value types.
	for _, d := range r.info.DeclList {
		if d.Kind == types.ValueDecl {
			if f := d.Field(n.Field); f != nil && identicalName(f.Type, n.FieldType) {
				return r.field(d, f)
			}
		}
	}
	return nil
}

func identicalName(a, b types.Type) bool { return a != nil && b != nil && a.String() == b.String() }

// uiArgParams are the parameters the argument list of a layout line takes:
// of the binding method, the screen or the primitive.
func (r *resolver) uiArgParams(n ast.Node) []*types.Var {
	u := r.info.UI
	if u == nil {
		return nil
	}
	switch n := n.(type) {
	case *ast.Binding:
		for un, sn := range u.Nodes {
			if pn, ok := un.(*ast.PrimNode); ok && pn.Binding == n {
				switch {
				case sn.Method != nil:
					return sn.Method.Params
				case sn.To != nil:
					return sn.To.Params
				}
			}
		}
	case *ast.PrimNode:
		sn := u.Nodes[n]
		switch {
		case sn == nil:
		case sn.Kind == sema.NodeScreen:
			return sn.To.Params
		case sn.Prim != nil:
			var out []*types.Var
			for _, p := range sn.Prim.Params {
				out = append(out, &types.Var{Name: p.Name, Type: p.Type})
			}
			return out
		}
	case *ast.OnNode:
		if sn := u.Nodes[n]; sn != nil && sn.To != nil {
			return sn.To.Params
		}
	}
	return nil
}

// layout offers completion in a layout line: primitives, screens, parts of
// the parent primitive, field names by the part's binds (RFC §22.7).
func (c *completer) layout(path []ast.Node, line int) {
	u := c.r.info.UI
	if u == nil {
		return
	}
	var parent *sema.Node
	for _, n := range path {
		if un, ok := n.(ast.UINode); ok {
			if sn := u.Nodes[un]; sn != nil && c.r.m.Module.FileSet.Position(un.Pos()).Line < line {
				parent = sn
			}
		}
	}
	for _, p := range u.Primitives {
		c.add(CompletionItem{Label: p.Name, Kind: kindClass, Detail: "primitive", SortText: "5" + p.Name})
	}
	for _, s := range u.Screens {
		c.add(CompletionItem{Label: s.Name, Kind: kindStruct, Detail: "screen", SortText: "6" + s.Name})
	}
	if parent == nil {
		return
	}
	if parent.Prim != nil && parent.Kind != sema.NodePart {
		for _, pt := range parent.Prim.Parts {
			c.add(CompletionItem{Label: pt.Name, Kind: kindProperty, Detail: "part", SortText: "1" + pt.Name})
		}
		if len(parent.Prim.Events) > 0 {
			c.keywords([]string{"on"})
		}
	}
	for _, name := range parent.LineNames() {
		c.add(CompletionItem{Label: name.Name, Kind: kindField, Detail: typeString(name.Type), SortText: "0" + name.Name})
	}
}

// uiSource is text from the module or from the standard UI catalog.
func (r *resolver) uiSource(rg source.Range) string {
	if s := r.m.Source(rg); s != "" {
		return s
	}
	if f := r.m.Module.Std; f != nil && f.Src.Contains(rg.Start) {
		src := f.Src.Source()
		return string(src[f.Src.Offset(rg.Start):f.Src.Offset(rg.End)])
	}
	return ""
}
