package lsp

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/types"
	"strings"
)

// fileRef is a module file and the model it is checked in.
type fileRef struct {
	path  string
	file  *module.File
	model *pipeline.Model
}

// pathAt returns the file nodes containing a position, from the file to
// the innermost. If there is no name at the position but there is one right
// before it (the cursor stands right after a name), that one is taken.
func pathAt(f *ast.File, pos source.Pos) []ast.Node {
	path := nodesAt(f, pos)
	if _, ok := last(path).(*ast.Ident); !ok && pos > f.Pos() {
		if prev := nodesAt(f, pos-1); len(prev) > 0 {
			if _, ok := last(prev).(*ast.Ident); ok {
				return prev
			}
		}
	}
	return path
}

func nodesAt(f *ast.File, pos source.Pos) []ast.Node {
	var path []ast.Node
	ast.Inspect(f, func(n ast.Node) bool {
		if n == nil || isNil(n) {
			return false
		}
		if _, ok := n.(*ast.File); !ok && (pos < n.Pos() || pos >= n.End()) {
			return false
		}
		path = append(path, n)
		return true
	})
	return path
}

// isNil: an interface with a nil pointer inside (optional parts of nodes).
func isNil(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.Ident:
		return n == nil
	case *ast.CommentGroup:
		return n == nil
	}
	return false
}

func last(path []ast.Node) ast.Node {
	if len(path) == 0 {
		return nil
	}
	return path[len(path)-1]
}

// parent returns the node i levels above the innermost one, or nil.
func parent(path []ast.Node, i int) ast.Node {
	if len(path) <= i {
		return nil
	}
	return path[len(path)-1-i]
}

// targetKind is what a name refers to.
type targetKind uint8

const (
	tLocal     targetKind = iota // let, a for variable, a shared test let
	tParam                       // a method or view parameter
	tField                       // a field
	tEnumValue                   // an enum value
	tBuiltin                     // a built-in value, function or method
	tDecl                        // a declaring type
	tMethod                      // a command, event, error, query…
	tIndex                       // an index
	tRole                        // a role
	tNamespace                   // a system or bounded context: def is the first declaration of the context
	tFeature                     // a system feature flag
)

// target is what a name in the source refers to.
type target struct {
	kind   targetKind
	name   string
	decl   *types.Decl      // the type; the owner of a field, method, index, parameter
	method *types.Method    // tMethod; the method of a parameter
	v      *types.Var       // tField, tParam
	value  *types.EnumValue // tEnumValue
	index  *types.Index     // tIndex
	role   *ast.RoleDecl    // tRole
	flag   *ast.FeatureDecl // tFeature
	typ    types.Type       // the type of the value
	def    *ast.Ident       // the name in the declaration; nil for built-in or unknown
}

// resolver resolves the names of one model.
type resolver struct {
	m    *pipeline.Model
	info *sema.Info
	// Owners of fields and parameters: for hover and references.
	fieldOwner map[*types.Var]*types.Decl
	paramOwner map[*types.Var]*types.Method
	methodOf   map[*types.Method]*types.Decl
}

func newResolver(m *pipeline.Model) *resolver {
	r := &resolver{m: m, info: m.Info,
		fieldOwner: map[*types.Var]*types.Decl{},
		paramOwner: map[*types.Var]*types.Method{},
		methodOf:   map[*types.Method]*types.Decl{},
	}
	for _, d := range m.Info.DeclList {
		for _, f := range d.Fields {
			r.fieldOwner[f] = d
		}
		for _, p := range d.Params {
			r.fieldOwner[p] = d
		}
		for _, md := range d.Methods {
			r.methodOf[md] = d
			for _, p := range md.Params {
				r.paramOwner[p] = md
			}
		}
	}
	return r
}

// at resolves the name at a position: the node path and the target; nil if
// it is not a name or the name is unknown.
func (r *resolver) at(f *ast.File, pos source.Pos) (*ast.Ident, *target) {
	path := pathAt(f, pos)
	id, ok := last(path).(*ast.Ident)
	if !ok {
		return nil, nil
	}
	return id, r.resolve(path)
}

// resolve resolves a name: the last node of the path.
func (r *resolver) resolve(path []ast.Node) *target {
	id := last(path).(*ast.Ident)
	info := r.info
	if o := info.Uses[id]; o != nil {
		return r.object(o)
	}
	if a, ok := info.Access[id]; ok {
		switch a.Kind {
		case sema.AccessField:
			return r.field(r.fieldOwner[a.Field], a.Field)
		case sema.AccessActor:
			return r.decl(a.Actor)
		case sema.AccessRole:
			return &target{kind: tRole, name: a.Role.Name.Name, role: a.Role, def: a.Role.Name}
		}
		return nil
	}
	if t, ok := r.uiTarget(path, id); ok {
		return t
	}
	switch p := parent(path, 1).(type) {
	case *ast.CallExpr:
		if p.Fun == id {
			return r.call(p)
		}
	case *ast.SelectorExpr:
		if p.Sel == id {
			if c, ok := parent(path, 2).(*ast.CallExpr); ok && c.Fun == p {
				return r.call(c)
			}
			if t := r.qualified(path, id); t != nil {
				return t // Ordering.OrderStatus: a type by full path
			}
			return r.member(info.Types[p.X], id.Name)
		}
	case *ast.Arg:
		if p.Name == id {
			return r.arg(path, id.Name)
		}
	case *ast.NamedType:
		return r.typePath(p, id)
	case *ast.Param:
		if p.Name == id {
			return r.param(path, p)
		}
	case *ast.EnumValue:
		if d := info.Lookup(enclosingDeclName(path)); d != nil {
			if v := d.Value(id.Name); v != nil {
				return &target{kind: tEnumValue, name: v.Name, decl: d, value: v, typ: &types.Enum{Decl: d}, def: v.Node.Name}
			}
		}
		return nil
	case *ast.OnDecl:
		if id != p.Event {
			return r.pathPart(p, p.Path, id) // on Ordering.Order.placed: the bounded context, the model
		}
		if ev := info.On[p]; ev != nil {
			return r.method(ev.Model, ev.Event)
		}
		if d := info.Owner[p]; d != nil {
			if md := d.Method(id.Name); md != nil {
				return r.method(d, md)
			}
		}
		return nil
	case *ast.IndexDecl:
		d := info.Owner[p]
		if d == nil {
			return nil
		}
		if p.Name == id {
			for _, ix := range d.Indexes {
				if ix.Node == p {
					return &target{kind: tIndex, name: ix.Name, decl: d, index: ix, def: p.Name}
				}
			}
			return nil
		}
		if md := d.Method(id.Name); md != nil {
			return r.method(d, md)
		}
		return nil
	case *ast.Path:
		// An index or search sort key: the first name is a record field.
		if p.Elems[0] != id {
			return nil
		}
		switch n := parent(path, 3).(type) {
		case *ast.IndexDecl:
			if d := info.Owner[n]; d != nil {
				return r.field(d, d.Field(id.Name))
			}
		case *ast.SearchExpr:
			if d := info.Lookup(n.Type.Name); d != nil {
				return r.member(&types.Record{Owner: d}, id.Name)
			}
		}
		return nil
	case *ast.RoleDecl:
		return &target{kind: tRole, name: id.Name, role: p, def: p.Name}
	case *ast.FeatureDecl:
		return &target{kind: tFeature, name: info.FeatureNames[p], flag: p, def: p.Name}
	case *ast.FieldDecl:
		if p.Name == id {
			if d := info.Owner[p]; d != nil {
				return r.field(d, d.Field(id.Name))
			}
		}
		return nil
	}
	if m, ok := parent(path, 1).(ast.Member); ok && memberIdent(m) == id {
		if d := info.Owner[m]; d != nil {
			if md := d.Method(id.Name); md != nil && md.Node == m {
				return r.method(d, md)
			}
		}
		return nil
	}
	// The start of a full path in an expression: Dispatch.DriverInfo(…), Ordering.OrderStatus.paid.
	if t := r.qualified(path, id); t != nil {
		return t
	}
	// A type name: in a declaration, in extend, in an expression (Status.paid, [Order]).
	if d, n := info.Resolve(id, []string{id.Name}); d != nil && n == 1 {
		return r.decl(d)
	}
	if d := info.Lookup(id.Name); d != nil {
		return r.decl(d)
	}
	return nil
}

// typePath is a part of a type: Ordering.CarClass, Auth.User.Id. Namespace
// parts lead to the bounded context, the type name to the declaration.
func (r *resolver) typePath(p *ast.NamedType, id *ast.Ident) *target {
	k := identIndex(p.Path, id)
	if k == len(p.Path)-1 && id.Name == "Id" && k > 0 {
		return r.pathPart(p, p.Path, p.Path[k-1])
	}
	return r.pathPart(p, p.Path, id)
}

// pathPart resolves part id of path elems: a declaration or a namespace.
func (r *resolver) pathPart(at ast.Node, elems []*ast.Ident, id *ast.Ident) *target {
	k := identIndex(elems, id)
	if k < 0 {
		return nil
	}
	names := make([]string, k+1)
	for i := range names {
		names[i] = elems[i].Name
	}
	if ns := r.info.Namespace(at, names); ns != "" {
		return r.namespace(ns)
	}
	if d, n := r.info.Resolve(at, names); d != nil && n == len(names) {
		return r.decl(d)
	}
	return nil
}

// qualified is a name in a full path of an expression (a chain x.y.z of names).
func (r *resolver) qualified(path []ast.Node, id *ast.Ident) *target {
	var top ast.Expr = id
	for i := 1; ; i++ {
		s, ok := parent(path, i).(*ast.SelectorExpr)
		if !ok || s.X != top && s.Sel != top {
			break
		}
		top = s
	}
	elems := identPath(top)
	if len(elems) < 2 {
		return nil
	}
	return r.pathPart(id, elems, id)
}

// namespace goes to a system or bounded context: the first declaration of its files.
func (r *resolver) namespace(ns string) *target {
	for _, f := range r.m.Program.Files {
		if f.Test || f.Context != ns && !strings.HasPrefix(f.Context, ns+".") {
			continue
		}
		for _, d := range f.AST.Decls {
			if id := declIdent(d); id != nil {
				return &target{kind: tNamespace, name: ns, def: id}
			}
		}
	}
	return nil
}

func identIndex(elems []*ast.Ident, id *ast.Ident) int {
	for i, e := range elems {
		if e == id {
			return i
		}
	}
	return -1
}

// identPath returns the names of x.y.z, or nil if it is not a chain of names.
func identPath(x ast.Expr) []*ast.Ident {
	switch x := x.(type) {
	case *ast.Ident:
		return []*ast.Ident{x}
	case *ast.SelectorExpr:
		if p := identPath(x.X); p != nil && !x.Safe {
			return append(p, x.Sel)
		}
	}
	return nil
}

func (r *resolver) object(o *sema.Object) *target {
	switch o.Kind {
	case sema.ObjLocal, sema.ObjFixture:
		def, _ := o.Node.(*ast.Ident)
		return &target{kind: tLocal, name: o.Name, typ: o.Type, def: def}
	case sema.ObjParam:
		t := &target{kind: tParam, name: o.Name, typ: o.Type, v: o.Var}
		if o.Var != nil {
			t.method = r.paramOwner[o.Var]
			t.decl = r.methodOf[t.method]
			if t.method == nil {
				t.decl = r.fieldOwner[o.Var] // a view parameter
			}
			if p, ok := o.Var.Node.(*ast.Param); ok {
				t.def = p.Name
			}
		}
		return t
	case sema.ObjField:
		if o.Var == nil {
			return &target{kind: tField, name: o.Name, typ: o.Type} // id
		}
		return r.field(r.fieldOwner[o.Var], o.Var)
	case sema.ObjEnumValue:
		return &target{kind: tEnumValue, name: o.Name, decl: o.Enum, value: o.Value, typ: o.Type, def: o.Value.Node.Name}
	case sema.ObjFeature:
		return &target{kind: tFeature, name: o.Name, flag: o.Feature, typ: o.Type, def: o.Feature.Name}
	}
	return &target{kind: tBuiltin, name: o.Name, typ: o.Type}
}

func (r *resolver) decl(d *types.Decl) *target {
	return &target{kind: tDecl, name: d.Name, decl: d, def: declIdent(d.Node)}
}

func (r *resolver) field(d *types.Decl, v *types.Var) *target {
	if v == nil {
		return nil
	}
	t := &target{kind: tField, name: v.Name, decl: d, v: v, typ: v.Type}
	switch n := v.Node.(type) {
	case *ast.FieldDecl:
		t.def = n.Name
	case *ast.Param:
		t.kind, t.def = tParam, n.Name
	}
	return t
}

func (r *resolver) method(d *types.Decl, md *types.Method) *target {
	return &target{kind: tMethod, name: md.Name, decl: d, method: md, typ: md.Result, def: memberIdent(md.Node)}
}

// call resolves the name of a callee: a method, a constructor, a projection, a built-in function.
func (r *resolver) call(c *ast.CallExpr) *target {
	call := r.info.Calls[c]
	if call == nil {
		return nil
	}
	switch call.Kind {
	case sema.CallMethod, sema.CallCommand, sema.CallQuery, sema.CallRequest, sema.CallExternal, sema.CallFake:
		if call.Method != nil {
			return r.method(call.Decl, call.Method)
		}
	case sema.CallConstruct, sema.CallUpsert, sema.CallConvert:
		return r.decl(call.Decl)
	case sema.CallBuiltin, sema.CallBuiltinMethod:
		return &target{kind: tBuiltin, name: call.Builtin, typ: r.info.Types[c]}
	}
	return nil
}

// member resolves x.name by the type of x: a value object or record field.
func (r *resolver) member(tx types.Type, name string) *target {
	switch t := types.Unwrap(tx).(type) {
	case *types.Value:
		return r.field(t.Decl, t.Decl.Field(name))
	case *types.Record:
		if name == "id" && t.Owner.ID != nil {
			return &target{kind: tField, name: name, decl: t.Owner, typ: t.Owner.ID}
		}
		return r.field(t.Owner, t.Owner.Field(name))
	}
	return nil
}

// arg resolves an argument name: a parameter of the called method or a
// field of the constructed object.
func (r *resolver) arg(path []ast.Node, name string) *target {
	switch n := parent(path, 3).(type) {
	case *ast.CallExpr:
		call := r.info.Calls[n]
		if call == nil {
			return nil
		}
		switch call.Kind {
		case sema.CallConstruct, sema.CallUpsert:
			return r.field(call.Decl, call.Decl.Field(name))
		}
		if call.Method != nil {
			return r.paramByName(call.Method, name)
		}
	case *ast.Binding, *ast.PrimNode, *ast.OnNode:
		for _, p := range r.uiArgParams(n) {
			if p.Name == name {
				if d := r.fieldOwner[p]; d != nil {
					return r.field(d, p)
				}
				if md := r.paramOwner[p]; md != nil {
					return r.paramByName(md, name)
				}
			}
		}
	case *ast.ThenError, *ast.ThenNotification, *ast.ThenRequest:
		if md := r.info.Then[n.(ast.ThenItem)]; md != nil {
			return r.paramByName(md, name)
		}
	}
	return nil
}

func (r *resolver) paramByName(md *types.Method, name string) *target {
	for _, p := range md.Params {
		if p.Name == name {
			t := &target{kind: tParam, name: name, decl: r.methodOf[md], method: md, v: p, typ: p.Type}
			if n, ok := p.Node.(*ast.Param); ok {
				t.def = n.Name
			}
			return t
		}
	}
	return nil
}

// param resolves a parameter in a method or view declaration.
func (r *resolver) param(path []ast.Node, p *ast.Param) *target {
	for i := 2; i < len(path); i++ {
		switch n := parent(path, i).(type) {
		case ast.Member:
			d := r.info.Owner[n]
			if d == nil {
				return nil
			}
			md := d.Method(memberIdent(n).String())
			if md == nil || md.Node != n {
				return nil
			}
			for _, v := range md.Params {
				if v.Node == p {
					return &target{kind: tParam, name: v.Name, decl: d, method: md, v: v, typ: v.Type, def: p.Name}
				}
			}
			return nil
		case *ast.ScreenDecl:
			if d := r.info.Lookup(n.Name.Name); d != nil {
				for _, v := range d.Params {
					if v.Node == p {
						return &target{kind: tParam, name: v.Name, decl: d, v: v, typ: v.Type, def: p.Name}
					}
				}
			}
			return nil
		}
	}
	return nil
}

// key is the declaration references are compared by: the name in the declaration.
func (t *target) key() *ast.Ident {
	if t.kind == tNamespace {
		return nil // a namespace has no declaration: no references are searched
	}
	return t.def
}

func enclosingDeclName(path []ast.Node) string {
	for _, n := range path {
		if d, ok := n.(ast.Decl); ok {
			return declIdent(d).String()
		}
	}
	return ""
}

// declIdent returns the name of a top-level declaration, or nil.
func declIdent(d ast.Decl) *ast.Ident {
	switch d := d.(type) {
	case *ast.EntityDecl:
		return d.Name
	case *ast.ProjectionDecl:
		return d.Name
	case *ast.ValueDecl:
		return d.Name
	case *ast.EnumDecl:
		return d.Name
	case *ast.ExternalDecl:
		return d.Name
	case *ast.AppDecl:
		return d.Name
	case *ast.ScreenDecl:
		return d.Name
	case *ast.PrimitiveDecl:
		return d.Name
	case *ast.RoleDecl:
		return d.Name
	case *ast.MigrationDecl:
		return d.Name
	case *ast.ExtendDecl:
		return d.Name
	}
	return nil
}

// memberIdent returns the name of a member, or nil.
func memberIdent(m ast.Node) *ast.Ident {
	switch n := m.(type) {
	case *ast.FieldDecl:
		return n.Name
	case *ast.IndexDecl:
		return n.Name
	case *ast.CommandDecl:
		return n.Name
	case *ast.EventDecl:
		return n.Name
	case *ast.NotificationDecl:
		return n.Name
	case *ast.ErrorDecl:
		return n.Name
	case *ast.QueryDecl:
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
