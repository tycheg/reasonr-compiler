// Package schemagen builds the model schema (model.Schema, RFC §30.5)
// from a checked model (RFC §28). The input is a
// model without errors.
package schemagen

import (
	"crypto/sha256"
	"github.com/tycheg/reasonr-compiler/internal/apicontract"
	"slices"
	"sort"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// Generate builds the model schema. Types are ordered by kind, then by
// name, and numbered from one; fields, parameters and indexes in
// declaration order (extension fields last); methods by kind, then by name.
func Generate(p *module.Program, info *sema.Info) *model.Schema {
	g := &gen{p: p, info: info, refs: map[*types.Decl]model.TypeRef{}}
	// The UI is not in the schema: it is a separate program (RFC §22).
	var decls []*types.Decl
	for _, d := range info.DeclList {
		if !d.Kind.IsUI() {
			decls = append(decls, d)
		}
	}
	sort.SliceStable(decls, func(i, j int) bool {
		a, b := typeKind(decls[i].Kind), typeKind(decls[j].Kind)
		if a != b {
			return a < b
		}
		return decls[i].Name < decls[j].Name
	})
	for i, d := range decls {
		g.refs[d] = model.TypeRef(i + 1)
	}
	s := &model.Schema{Extension: p.Extension, Types: []model.TypeInfo{}}
	for _, d := range decls {
		s.Types = append(s.Types, g.typeInfo(d))
	}
	for name := range info.Roles {
		s.Roles = append(s.Roles, name)
	}
	sort.Strings(s.Roles)
	for _, name := range sema.FeatureList(info) {
		f := info.Features[name]
		s.Features = append(s.Features, model.FeatureInfo{Name: name, Date: f.Date.Value, Doc: f.Doc.Text()})
	}
	for _, d := range p.Decls {
		if m, ok := d.(*ast.MigrationDecl); ok {
			s.Migrations = append(s.Migrations, m.Name.Name)
		}
	}
	sort.Strings(s.Migrations)
	// The external API contract (RFC §31.3.1): methods published by the
	// api/contract.json patterns, deprecations, version snapshots.
	// compat.API checks patterns without methods.
	s.API, _ = apicontract.Resolve(p.API(), s)
	return s
}

// Hash returns the SHA-256 of the schema in canonical JSON.
func Hash(s *model.Schema) ([32]byte, error) {
	b, err := s.MarshalIndent()
	if err != nil {
		return [32]byte{}, err
	}
	return sha256.Sum256(b), nil
}

type gen struct {
	p    *module.Program
	info *sema.Info
	refs map[*types.Decl]model.TypeRef
	ext  bool // the current type is changed by an extension
	// reactors are the ACL reactions by source aggregate (aclReactors).
	reactors map[*types.Decl][]aclOn
}

func typeKind(k types.DeclKind) model.TypeKind {
	return map[types.DeclKind]model.TypeKind{
		types.EntityDecl: model.TypeEntity, types.ActorDecl: model.TypeActor,
		types.ProjectionDecl: model.TypeProjection, types.ValueDecl: model.TypeValue,
		types.EnumDecl: model.TypeEnum, types.ExternalDecl: model.TypeExternal,
	}[k]
}

// extension returns the extension name if the node came from it.
func (g *gen) extension(n ast.Node) string {
	if n != nil && g.p.FromExtension(n) {
		g.ext = true
		return g.p.Extension
	}
	return ""
}

func (g *gen) typeInfo(d *types.Decl) model.TypeInfo {
	g.ext = g.p.FromExtension(d.Node)
	t := model.TypeInfo{Ref: g.refs[d], Name: d.Name, Kind: typeKind(d.Kind), Role: d.Role}
	if d.ID != nil {
		t.ID, t.IDType = g.idStrategy(d.ID)
	}
	for i, f := range d.Fields {
		t.Fields = append(t.Fields, g.field(d, i, f))
	}
	for i, ix := range d.Indexes {
		t.Indexes = append(t.Indexes, g.index(d, i, ix))
	}
	t.Methods = g.methods(d)
	g.aclReactors(d, t.Methods)
	if d.Owner != nil {
		t.Owner = g.refs[d.Owner]
	}
	g.innerParts(d, &t)
	for _, from := range d.Callers {
		t.Callers = append(t.Callers, g.refs[from])
	}
	for _, v := range d.Values {
		t.Enum = append(t.Enum, v.Name)
		if v.Node != nil && v.Node.Was != nil {
			if t.EnumWas == nil {
				t.EnumWas = make([]string, len(d.Values))
			}
			t.EnumWas[v.Index] = v.Node.Was.Name
		}
	}
	t.Extended = g.ext
	return t
}

// destructive reports whether a command body has forget or delete.
func destructive(body *ast.Block) bool {
	found := false
	if body != nil {
		ast.Inspect(body, func(n ast.Node) bool {
			switch n.(type) {
			case *ast.ForgetStmt, *ast.DeleteStmt:
				found = true
			}
			return !found
		})
	}
	return found
}

// aclReactors marks on the events of d the bounded contexts whose ACLs react to them.
func (g *gen) aclReactors(d *types.Decl, methods []model.MethodInfo) {
	if g.reactors == nil { // ACL reactions by source aggregate: one pass per schema
		g.reactors = map[*types.Decl][]aclOn{}
		for _, a := range g.info.ACLs {
			for _, m := range a.Node.Members {
				if on, ok := m.(*ast.OnDecl); ok && g.info.On[on] != nil {
					src := g.info.On[on].Model
					g.reactors[src] = append(g.reactors[src], aclOn{a.Context, on.Event.Name})
				}
			}
		}
	}
	for _, r := range g.reactors[d] {
		for i := range methods {
			mi := &methods[i]
			if mi.Kind == model.MethodEvent && mi.Name == r.event && !slices.Contains(mi.ACL, r.context) {
				mi.ACL = append(mi.ACL, r.context)
				sort.Strings(mi.ACL)
			}
		}
	}
}

// aclOn is the reaction of the ACL of bounded context context to event event.
type aclOn struct{ context, event string }

// innerParts adds its inner entities to an aggregate (docs/plans/0011,
// §3.5): a map field of records for each (after its own fields) and events
// "Stop.arrived" — an inner entity event with its id as the first argument.
func (g *gen) innerParts(d *types.Decl, t *model.TypeInfo) {
	for _, in := range d.Inner {
		id := g.typ(in.ID)
		t.Fields = append(t.Fields, model.FieldInfo{
			Ref: model.FieldRef(len(t.Fields)), Name: in.Short, Inner: g.refs[in],
			Type: model.Type{Kind: model.KindMap, Key: &id, Elem: &model.Type{Kind: model.KindRecord, Ref: g.refs[in]}},
		})
		for j, m := range sortedMethods(in) {
			if m.Kind != types.Event {
				continue
			}
			mi := model.MethodInfo{
				Ref: model.MethodRef(len(t.Methods)), Name: in.Short + "." + m.Name, Kind: model.MethodEvent,
				Inner: g.refs[in], InnerEvent: model.MethodRef(j), Extension: g.extension(m.Node),
				Params: []model.FieldInfo{{Name: "id", Type: id}},
			}
			for k, p := range m.Params {
				fi := g.param(in, k, p)
				fi.Ref = model.FieldRef(k + 1)
				mi.Params = append(mi.Params, fi)
			}
			t.Methods = append(t.Methods, mi)
		}
	}
}

func (g *gen) idStrategy(id *types.ID) (model.IDStrategy, model.TypeRef) {
	switch b := id.Base.(type) {
	case *types.Basic:
		switch b.Kind {
		case types.UUID:
			return model.IDUUID, 0
		case types.Bigint:
			return model.IDSequence, 0
		}
		return model.IDGiven, 0
	case *types.Value:
		return model.IDGiven, g.refs[b.Decl]
	case *types.ID:
		return model.IDDerived, g.refs[b.Owner]
	}
	return model.IDGiven, 0
}

// typ converts a model type into a schema type.
func (g *gen) typ(t types.Type) model.Type {
	switch t := t.(type) {
	case *types.Optional:
		out := g.typ(t.Elem)
		out.Optional = true
		return out
	case *types.Basic:
		return model.Type{Kind: basicKinds[t.Kind]}
	case *types.Enum:
		return model.Type{Kind: model.KindEnum, Ref: g.refs[t.Decl]}
	case *types.Value:
		return model.Type{Kind: model.KindRecord, Ref: g.refs[t.Decl]}
	case *types.Record:
		return model.Type{Kind: model.KindRecord, Ref: g.refs[t.Owner]}
	case *types.ID:
		return model.Type{Kind: model.KindID, Ref: g.refs[t.Owner]}
	case *types.TimerID:
		return model.Type{Kind: model.KindTimerID}
	case *types.PageReq:
		return model.Type{Kind: model.KindPageRequest}
	case *types.List:
		return g.container(model.KindList, t.Elem)
	case *types.Set:
		return g.container(model.KindSet, t.Elem)
	case *types.Page:
		return g.container(model.KindPage, t.Elem)
	case *types.Map:
		out := g.container(model.KindMap, t.Elem)
		key := g.typ(t.Key)
		out.Key = &key
		return out
	}
	return model.Type{Kind: model.KindNone}
}

func (g *gen) container(k model.Kind, elem types.Type) model.Type {
	e := g.typ(elem)
	return model.Type{Kind: k, Elem: &e}
}

var basicKinds = map[types.BasicKind]model.Kind{
	types.Int: model.KindInt, types.Bigint: model.KindBigint, types.Float: model.KindFloat,
	types.Decimal: model.KindDecimal, types.String: model.KindString, types.Bool: model.KindBool,
	types.UUID: model.KindUUID, types.Date: model.KindDate, types.Time: model.KindTime,
	types.Timestamp: model.KindTimestamp, types.Duration: model.KindDuration, types.Bytes: model.KindBytes,
}

func (g *gen) field(d *types.Decl, i int, f *types.Var) model.FieldInfo {
	fd := f.Node.(*ast.FieldDecl)
	out := model.FieldInfo{Ref: model.FieldRef(i), Name: f.Name, Type: g.typ(f.Type), Extension: g.extension(fd)}
	if v, ok := g.info.Defaults[f]; ok {
		out.Default = g.literal(v, f.Type)
	}
	if fd.Was != nil {
		out.WasName = fd.Was.Name
	}
	if f.Personal {
		out.Personal = g.personal(d, fd.Personal, d)
	}
	return out
}

// param is a parameter of a method or a view; owner is the type that owns
// the method.
func (g *gen) param(owner *types.Decl, i int, p *types.Var) model.FieldInfo {
	out := model.FieldInfo{Ref: model.FieldRef(i), Name: p.Name, Type: g.typ(p.Type)}
	if v, ok := g.info.Defaults[p]; ok {
		out.Default = g.literal(v, p.Type)
	}
	if n, ok := p.Node.(*ast.Param); ok && n.Was != nil {
		out.WasName = n.Was.Name
	}
	if n, ok := p.Node.(*ast.Param); ok && p.Personal {
		self := owner
		if owner.Kind == types.ExternalDecl {
			self = nil // a personal parameter of an external request: any subject
		}
		out.Personal = g.personal(owner, n.Personal, self)
	}
	return out
}

// personal is the subject: the entity itself (self) or the entity from a path field.
func (g *gen) personal(owner *types.Decl, pers *ast.Personal, self *types.Decl) *model.Personal {
	if pers == nil || pers.Subject == nil {
		return &model.Personal{SubjectType: g.refs[self]}
	}
	path, t := g.path(owner, pers.Subject.Elems)
	out := &model.Personal{Subject: path}
	if id, ok := types.Unwrap(t).(*types.ID); ok {
		out.SubjectType = g.refs[id.Owner]
	}
	return out
}

// path converts a path through fields into field numbers and returns the type of the last one.
func (g *gen) path(d *types.Decl, elems []*ast.Ident) (model.FieldPath, types.Type) {
	var out model.FieldPath
	var t types.Type
	owner := d
	for _, e := range elems {
		if owner == nil {
			break
		}
		i := fieldIndex(owner, e.Name)
		if i < 0 {
			break
		}
		out = append(out, model.FieldRef(i))
		t = owner.Fields[i].Type
		owner = nil
		if v, ok := types.Unwrap(t).(*types.Value); ok {
			owner = v.Decl
		}
	}
	return out, t
}

func fieldIndex(d *types.Decl, name string) int {
	for i, f := range d.Fields {
		if f.Name == name {
			return i
		}
	}
	return -1
}

func (g *gen) index(d *types.Decl, i int, ix *types.Index) model.IndexInfo {
	n := ix.Node
	g.extension(n)
	out := model.IndexInfo{Ref: model.IndexRef(i), Name: ix.Name, Unique: n.IsUnique()}
	for _, k := range n.Keys {
		path, _ := g.path(d, k.Path.Elems)
		out.Keys = append(out.Keys, model.SortKey{Field: path, Desc: k.Desc()})
	}
	if n.Error != nil {
		out.Error = n.Error.Name
	}
	return out
}

var methodKinds = map[types.MethodKind]model.MethodKind{
	types.Command: model.MethodCommand, types.Event: model.MethodEvent,
	types.Notification: model.MethodNotification, types.Error: model.MethodError,
	types.Query: model.MethodQuery, types.Rule: model.MethodRule,
	types.Request: model.MethodRequest, types.ValueMethod: model.MethodValue,
}

// sortedMethods returns methods by kind, then by name.
func sortedMethods(d *types.Decl) []*types.Method {
	list := append([]*types.Method(nil), d.Methods...)
	sort.SliceStable(list, func(i, j int) bool {
		a, b := methodKinds[list[i].Kind], methodKinds[list[j].Kind]
		if a != b {
			return a < b
		}
		return list[i].Name < list[j].Name
	})
	return list
}

func (g *gen) methods(d *types.Decl) []model.MethodInfo {
	var out []model.MethodInfo
	for i, m := range sortedMethods(d) {
		mi := model.MethodInfo{
			Ref: model.MethodRef(i), Name: m.Name, Kind: methodKinds[m.Kind], Public: m.Public(), Extension: g.extension(m.Node),
		}
		for j, p := range m.Params {
			mi.Params = append(mi.Params, g.param(d, j, p))
		}
		if _, void := m.Result.(*types.Void); !void && m.Result != nil {
			r := g.typ(m.Result)
			mi.Result = &r
		}
		switch n := m.Node.(type) {
		case *ast.CommandDecl:
			mi.Access = g.access(n.Access, mi.Public)
			mi.Create = n.Create.IsValid()
			if mi.Public {
				mi.Doc = n.Doc.Text()
				mi.Destructive = destructive(n.Body)
			}
			if n.Was != nil {
				mi.Was = n.Was.Name
			}
		case *ast.QueryDecl:
			mi.Access = g.access(n.Access, mi.Public)
			if mi.Public {
				mi.Doc = n.Doc.Text()
			}
			if n.Was != nil {
				mi.Was = n.Was.Name
			}
		case *ast.RequestDecl:
			for j, p := range m.Params {
				if p.Name == sema.TimeoutParam {
					mi.TimeoutParam = j + 1
				}
			}
			for _, decl := range g.info.Impls {
				if decl == m {
					mi.Fake = true
				}
			}
		}
		out = append(out, mi)
	}
	return out
}

// access converts an access list; a public method without a list is open to
// any authenticated caller.
func (g *gen) access(list *ast.AccessList, public bool) []model.Access {
	if list == nil {
		if public {
			return []model.Access{{Kind: model.AccessAny}}
		}
		return nil
	}
	var out []model.Access
	for _, id := range list.Names {
		a, ok := g.info.Access[id]
		if !ok {
			continue
		}
		switch a.Kind {
		case sema.AccessSelf:
			out = append(out, model.Access{Kind: model.AccessSelf})
		case sema.AccessField:
			owner := g.info.Owner[a.Field.Node.(*ast.FieldDecl)]
			out = append(out, model.Access{Kind: model.AccessField, Field: model.FieldPath{model.FieldRef(fieldIndex(owner, a.Field.Name))}})
		case sema.AccessActor:
			out = append(out, model.Access{Kind: model.AccessActor, Actor: g.refs[a.Actor]})
		case sema.AccessRole:
			out = append(out, model.Access{Kind: model.AccessRole, Role: g.info.RoleNames[a.Role]})
		}
	}
	return out
}
