// Package lower lowers a checked model (AST and sema.Info) to IR
// (RFC §28). The input is a model without
// errors: lowering reports no diagnostics and relies on the types and
// resolved names from Info.
package lower

import (
	"fmt"
	"strconv"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/check"
	"github.com/tycheg/reasonr-compiler/internal/ir"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// Program lowers a model to IR. checks are the analyzer results: searches
// (check.Queries) and personal data subjects of arguments (check.PII).
func Program(p *module.Program, info *sema.Info, checks *check.Result) *ir.Program {
	l := &lowerer{p: p, info: info, searches: checks.Searches(), out: &ir.Program{Extension: p.Extension}, files: map[string]int{}}
	if pm, ok := checks.Of(check.PII).(*check.PIIMap); ok {
		l.sites = pm.Sites
	}
	l.reactions()
	for _, d := range info.DeclList {
		l.decl(d)
	}
	for _, a := range info.ACLs {
		l.acl(a)
	}
	var fixtures []*ast.LetStmt
	for _, d := range p.Decls {
		if ld, ok := d.(*ast.LetDecl); ok {
			fixtures = append(fixtures, ld.Let)
		}
	}
	for _, d := range p.Decls {
		switch d := d.(type) {
		case *ast.MigrationDecl:
			f := l.newFn(ir.FuncMigration, "migration."+d.Name.Name, nil, nil)
			f.prologue(d.Body)
			f.block(d.Body)
			l.finish(f)
		case *ast.TestDecl:
			l.test(d, fixtures)
		}
	}
	l.uiFuncs()
	return l.out
}

type lowerer struct {
	p        *module.Program
	info     *sema.Info
	searches map[*ast.SearchExpr]*check.Search
	sites    map[*ast.CallExpr][]check.SiteSubject
	out      *ir.Program
	owners   map[*types.Method]*types.Decl // ownerOf
	// reacted are the events with on reactions: owner → names.
	reacted map[*types.Decl]map[string]bool
	// onNames are the names of reaction functions.
	onNames map[*ast.OnDecl]string
	// files are file numbers in out.Files (from 1) by path relative to the module.
	files map[string]int
}

// position returns the file (a number in out.Files from 1) and the line of
// position pos; 0, 0 for a position outside the model files.
func (l *lowerer) position(pos source.Pos) (file, line int) {
	if !pos.IsValid() {
		return 0, 0
	}
	mf := l.p.FileAt(pos)
	if mf == nil {
		return 0, 0
	}
	n, ok := l.files[mf.Rel]
	if !ok {
		l.out.Files = append(l.out.Files, mf.Rel)
		n = len(l.out.Files)
		l.files[mf.Rel] = n
	}
	return n, mf.Src.Line(pos)
}

// reactions collects the reaction table and the names of their functions.
func (l *lowerer) reactions() {
	l.reacted = map[*types.Decl]map[string]bool{}
	l.onNames = map[*ast.OnDecl]string{}
	byEvent := map[string]*ir.Reaction{}
	for _, d := range l.info.DeclList {
		count := map[string]int{}
		for _, m := range members(d) {
			on, ok := m.(*ast.OnDecl)
			if !ok {
				continue
			}
			ev := on.Event.Name
			count[ev]++
			name := d.Name + ".on." + ev
			if count[ev] > 1 {
				name += "#" + strconv.Itoa(count[ev])
			}
			l.onNames[on] = name
			src := d // the entity whose event triggers the reaction
			if l.reacted[src] == nil {
				l.reacted[src] = map[string]bool{}
			}
			l.reacted[src][ev] = true
			key := src.Name + "." + ev
			r := byEvent[key]
			if r == nil {
				r = &ir.Reaction{Event: key}
				byEvent[key] = r
				l.out.Reactions = append(l.out.Reactions, r)
			}
			r.Funcs = append(r.Funcs, name)
		}
	}
	// ACL reactions: the owner of the function is the source entity, the
	// name is "Ordering.Order.on.placed@Dispatch".
	for _, a := range l.info.ACLs {
		for _, m := range a.Node.Members {
			on, ok := m.(*ast.OnDecl)
			if !ok || l.info.On[on] == nil {
				continue
			}
			src := l.info.On[on].Model
			ev := on.Event.Name
			name := src.Name + ".on." + ev + "@" + a.Context
			l.onNames[on] = name
			if l.reacted[src] == nil {
				l.reacted[src] = map[string]bool{}
			}
			l.reacted[src][ev] = true
			key := src.Name + "." + ev
			r := byEvent[key]
			if r == nil {
				r = &ir.Reaction{Event: key}
				byEvent[key] = r
				l.out.Reactions = append(l.out.Reactions, r)
			}
			r.Funcs = append(r.Funcs, name)
		}
	}
}

// acl lowers ACL reactions: the body runs with a snapshot of the source
// entity (id is its identifier), the parameters are the event arguments.
func (l *lowerer) acl(a *sema.ACL) {
	for _, m := range a.Node.Members {
		on, ok := m.(*ast.OnDecl)
		if !ok || l.info.On[on] == nil {
			continue
		}
		t := l.info.On[on]
		f := l.newFn(ir.FuncACL, l.onNames[on], t.Model, t.Event.Params)
		f.prologue(on.Body)
		f.block(on.Body)
		l.finish(f)
	}
}

func members(d *types.Decl) []ast.Member {
	switch n := d.Node.(type) {
	case *ast.EntityDecl:
		return n.Members
	case *ast.ProjectionDecl:
		return n.Members
	case *ast.ValueDecl:
		return n.Members
	case *ast.ExternalDecl:
		return n.Members
	}
	return nil
}

func (l *lowerer) decl(d *types.Decl) {
	var checks []*ast.CheckDecl
	for _, m := range members(d) {
		switch m := m.(type) {
		case *ast.CommandDecl:
			l.method(d, m.Name, ir.FuncCommand, m.Access, m.Body)
		case *ast.EventDecl:
			if m.Body != nil {
				l.method(d, m.Name, ir.FuncApply, nil, m.Body)
			}
		case *ast.QueryDecl:
			l.method(d, m.Name, ir.FuncQuery, m.Access, m.Body)
		case *ast.RuleDecl:
			l.method(d, m.Name, ir.FuncRule, nil, m.Body)
		case *ast.MethodDecl:
			l.method(d, m.Name, ir.FuncMethod, nil, m.Body)
		case *ast.OnDecl:
			ev := d.Method(m.Event.Name)
			f := l.newFn(ir.FuncReaction, l.onNames[m], d, ev.Params)
			f.reacts = l.raisesReacted(d, m.Body)
			f.prologue(m.Body)
			f.block(m.Body)
			l.finish(f)
		case *ast.CheckDecl:
			checks = append(checks, m)
		case *ast.RequestDecl:
			if m.Body != nil {
				l.fakeRequest(d, m)
			}
		}
	}
	if len(checks) > 0 {
		l.checks(d, checks)
	}
}

// method lowers a member with a body.
func (l *lowerer) method(d *types.Decl, name *ast.Ident, kind ir.FuncKind, access *ast.AccessList, body *ast.Block) {
	m := d.Method(name.Name)
	if m == nil {
		panic(fmt.Sprintf("lower: no method %s.%s", d.Name, name.Name))
	}
	f := l.newFn(kind, d.Name+"."+m.Name, d, m.Params)
	if _, void := m.Result.(*types.Void); !void && m.Result != nil {
		f.f.Result = irType(m.Result)
	}
	f.f.Access = l.access(access)
	if kind == ir.FuncCommand {
		f.reacts = l.raisesReacted(d, body)
	}
	f.prologue(body)
	f.block(body)
	l.finish(f)
}

// fakeRequest lowers a request implementation in a fake: a command with the
// method of the declared request. The function parameters are the request
// parameters, timeout included: the engine passes the request arguments in
// full; the implementation parameters take the slots of the same names.
func (l *lowerer) fakeRequest(d *types.Decl, m *ast.RequestDecl) {
	decl := l.info.Impls[m]
	f := l.newFn(ir.FuncCommand, d.Name+"."+decl.Name, d, decl.Params)
	for _, p := range m.Params.List {
		for _, dp := range decl.Params {
			if dp.Name == p.Name.Name {
				f.slots[p] = f.slots[dp.Node]
			}
		}
	}
	f.prologue(m.Body)
	f.block(m.Body)
	l.finish(f)
}

// checks lowers the invariants of a value object into a function with a bool result.
func (l *lowerer) checks(d *types.Decl, list []*ast.CheckDecl) {
	f := l.newFn(ir.FuncCheck, d.Name+".check", d, nil)
	f.f.Result = ir.Basic(ir.TBool)
	fail := f.newBlock()
	for _, c := range list {
		next := f.newBlock()
		f.at(c.Pos())
		f.cond(c.Cond, next, fail)
		f.setBlock(next)
	}
	f.emit(ir.Instr{Op: ir.OpRetVal, Args: []ir.Operand{f.konst(boolConst(true))}})
	f.setBlock(fail)
	f.emit(ir.Instr{Op: ir.OpRetVal, Args: []ir.Operand{f.konst(boolConst(false))}})
	l.finish(f)
}

// raisesReacted reports whether a body emits events that have reactions.
func (l *lowerer) raisesReacted(d *types.Decl, body ast.Node) bool {
	evs := l.reacted[d]
	if len(evs) == 0 {
		return false
	}
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if x, ok := n.(*ast.CallExpr); ok {
			if c := l.info.Calls[x]; c != nil && c.Kind == sema.CallMethod && c.Method.Kind == types.Event && evs[c.Method.Name] {
				found = true
			}
		}
		return !found
	})
	return found
}

func (l *lowerer) access(list *ast.AccessList) []ir.Access {
	if list == nil {
		return nil
	}
	var out []ir.Access
	for _, id := range list.Names {
		a, ok := l.info.Access[id]
		if !ok {
			continue
		}
		switch a.Kind {
		case sema.AccessSelf:
			out = append(out, ir.Access{Kind: ir.AccessSelf})
		case sema.AccessField:
			out = append(out, ir.Access{Kind: ir.AccessField, Name: a.Field.Name})
		case sema.AccessActor:
			out = append(out, ir.Access{Kind: ir.AccessActor, Name: a.Actor.Name})
		case sema.AccessRole:
			out = append(out, ir.Access{Kind: ir.AccessRole, Name: l.info.RoleNames[a.Role]})
		}
	}
	return out
}

func (l *lowerer) finish(f *fn) {
	f.finish()
	ir.Optimize(f.f)
	l.out.Funcs = append(l.out.Funcs, f.f)
}

// ownerOf returns the type that owns a method.
func (l *lowerer) ownerOf(m *types.Method) *types.Decl {
	if l.owners == nil { // one pass per program (docs/plans/0015)
		l.owners = map[*types.Method]*types.Decl{}
		for _, d := range l.info.DeclList {
			for _, dm := range d.Methods {
				if d.Method(dm.Name) == dm && l.owners[dm] == nil {
					l.owners[dm] = d
				}
			}
		}
	}
	return l.owners[m]
}

// irType converts a model type into an IR type.
func irType(t types.Type) *ir.Type {
	switch t := t.(type) {
	case *types.Basic:
		return ir.Basic(basicKinds[t.Kind])
	case *types.Optional:
		return ir.Optional(irType(t.Elem))
	case *types.List:
		return &ir.Type{Kind: ir.TList, Elem: irType(t.Elem)}
	case *types.Set:
		return &ir.Type{Kind: ir.TSet, Elem: irType(t.Elem)}
	case *types.Map:
		return &ir.Type{Kind: ir.TMap, Key: irType(t.Key), Elem: irType(t.Elem)}
	case *types.Page:
		return &ir.Type{Kind: ir.TPage, Elem: irType(t.Elem)}
	case *types.Enum:
		return &ir.Type{Kind: ir.TEnum, Name: t.Decl.Name}
	case *types.Value:
		return &ir.Type{Kind: ir.TValue, Name: t.Decl.Name}
	case *types.ID:
		return &ir.Type{Kind: ir.TID, Name: t.Owner.Name}
	case *types.TimerID:
		return ir.Basic(ir.TTimer)
	case *types.Ref:
		return &ir.Type{Kind: ir.TRef, Name: t.Owner.Name}
	case *types.Record:
		return &ir.Type{Kind: ir.TRecord, Name: t.Owner.Name}
	case *types.Caller:
		return ir.Basic(ir.TCaller)
	case *types.PageReq:
		return ir.Basic(ir.TPageReq)
	}
	panic(fmt.Sprintf("lower: type %v is not lowered", t))
}

var basicKinds = map[types.BasicKind]ir.TypeKind{
	types.Int: ir.TInt, types.Bigint: ir.TBigint, types.Float: ir.TFloat, types.Decimal: ir.TDecimal,
	types.String: ir.TString, types.Bool: ir.TBool, types.UUID: ir.TUUID, types.Date: ir.TDate,
	types.Time: ir.TTime, types.Timestamp: ir.TTimestamp, types.Duration: ir.TDuration, types.Bytes: ir.TBytes,
}
