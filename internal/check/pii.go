package check

import (
	"sort"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// PII tracks personal data flows (plan, §12.7): a value from a personal
// source goes only into a personal place with the same subject. Explicit
// flows are tracked: assignments and arguments. The result is the personal
// data map *PIIMap for reasonr pii.
var PII = &Analyzer{
	Name:  "pii",
	Doc:   "personal data goes only into personal places of the same subject",
	Codes: []diag.Code{"E0901", "E0902", "E0903", "E0904", "E0906", "W1020", "W1021"},
	Run:   runPII,
}

// PIIMap is the personal data map of a model.
type PIIMap struct {
	Fields  []PersonalVar // personal fields
	Params  []PersonalVar // personal method parameters
	Flows   []Flow        // where personal values go
	Forgets []Forget      // where subjects are forgotten
	// Sites are the personal data subjects in the arguments of external
	// requests, notifications and errors of commands and reactions (RFC §20.2,
	// rule 7): the engine encrypts these arguments with them
	// in the outbox and in responses by key.
	Sites map[*ast.CallExpr][]SiteSubject
}

// SiteSubject is the personal data subject in a call argument: its id lies
// in the fields of self at Path (nil means the entity self itself).
type SiteSubject struct {
	Arg     string // parameter name
	Subject *types.Decl
	Path    []string
}

// PersonalVar is a personal field or parameter.
type PersonalVar struct {
	Owner   *types.Decl
	Method  *types.Method // for a parameter
	Var     *types.Var
	Subject *types.Decl // nil: any subject (an external request parameter)
}

// FlowKind is where a value goes.
type FlowKind uint8

const (
	FlowEvent        FlowKind = iota // an event argument
	FlowField                        // a field (in an event)
	FlowCommand                      // a command argument
	FlowRequest                      // an external system request argument
	FlowUpsert                       // a projection field
	FlowNotification                 // a notification argument
	FlowResult                       // a query result
	FlowError                        // an error argument
)

// Flow is a personal data flow: in member Member of type In, a value with
// subjects Subjects goes into Target under name Name.
type Flow struct {
	In       *types.Decl
	Member   ast.Member
	Subjects []*types.Decl
	Kind     FlowKind
	Target   string // "Type.method" or a type (a field, a projection)
	Name     string // an argument or a field; "" for a query result
	Node     ast.Node
}

// Forget is a forget of a subject in a member.
type Forget struct {
	In      *types.Decl
	Member  ast.Member
	Subject *types.Decl
	Node    ast.Node
}

// origin is the personal data source of a value: the subject and the path
// to its id in the fields of self ("" is the entity itself). foreign: the
// value comes from a record field (c.phone in a query) rather than from
// self, so there is no path from self. subject nil: a personal place
// without a definite subject (an external request parameter).
type origin struct {
	subject *types.Decl
	path    string
	foreign bool
}

// label is the personal data sources of a value.
type label map[origin]bool

// subjects returns the subjects of a label.
func (l label) subjects() map[*types.Decl]bool {
	out := map[*types.Decl]bool{}
	for s := range l {
		out[s.subject] = true
	}
	return out
}

func (l label) add(o label) label {
	if len(o) == 0 {
		return l
	}
	if l == nil {
		l = label{}
	}
	for k := range o {
		l[k] = true
	}
	return l
}

func (l label) String() string {
	names := make([]string, 0, len(l))
	for d := range l.subjects() {
		if d != nil {
			names = append(names, d.Name)
		}
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

type piiPass struct {
	*Pass
	owners map[*ast.Param]*types.Decl // parameter → the type that owns the method
	locals map[ast.Node]label         // labels of local variables (by declaration)
	out    *PIIMap
	cur    member // the member being checked
}

// flow records the flow of a value with label l.
func (p *piiPass) flow(l label, kind FlowKind, target, name string, node ast.Node) {
	var subjects []*types.Decl
	for d := range l.subjects() {
		subjects = append(subjects, d)
	}
	sort.Slice(subjects, func(i, j int) bool { return subjects[i].Name < subjects[j].Name })
	p.out.Flows = append(p.out.Flows, Flow{In: p.cur.decl, Member: p.cur.node, Subjects: subjects, Kind: kind, Target: target, Name: name, Node: node})
}

func runPII(pass *Pass) any {
	p := &piiPass{Pass: pass, owners: map[*ast.Param]*types.Decl{}, locals: map[ast.Node]label{}, out: &PIIMap{Sites: map[*ast.CallExpr][]SiteSubject{}}}
	subjects := map[*types.Decl]bool{}
	for _, d := range pass.Info.DeclList {
		for _, f := range d.Fields {
			if f.Personal {
				subjects[p.fieldSubject(f)] = true
				p.optional(f.Node.(*ast.FieldDecl).Name, f.Type, d.Kind.HasID())
				p.out.Fields = append(p.out.Fields, PersonalVar{Owner: d, Var: f, Subject: p.fieldSubject(f)})
			}
		}
		for _, m := range d.Methods {
			for _, prm := range m.Params {
				if n, ok := prm.Node.(*ast.Param); ok {
					p.owners[n] = d
					if prm.Personal {
						subjects[p.paramSubject(prm)] = true
						p.optional(n.Name, prm.Type, m.Kind == types.Event)
						p.out.Params = append(p.out.Params, PersonalVar{Owner: d, Method: m, Var: prm, Subject: p.paramSubject(prm)})
					}
				}
			}
		}
	}
	for _, m := range bodies(pass.Info) {
		p.cur = m
		p.body(m, subjects)
	}
	return p.out
}

// optional: personal fields and event parameters must be T? (E0904).
func (p *piiPass) optional(name *ast.Ident, t types.Type, must bool) {
	if !must || types.IsInvalid(t) {
		return
	}
	if _, ok := t.(*types.Optional); !ok {
		p.Report(diag.E0904, rangeOf(name), name.Name, t.String())
	}
}

// fieldSubject is the subject of a personal field: the entity itself or the
// entity whose identifier lies in the field from personal(...).
func (p *piiPass) fieldSubject(f *types.Var) *types.Decl {
	fd := f.Node.(*ast.FieldDecl)
	owner := p.Info.Owner[fd]
	return p.subject(owner, fd.Personal)
}

func (p *piiPass) paramSubject(v *types.Var) *types.Decl {
	n := v.Node.(*ast.Param)
	owner := p.owners[n]
	if n.Personal == nil {
		return owner
	}
	if owner != nil && owner.Kind == types.ExternalDecl && n.Personal.Subject == nil {
		return nil
	}
	return p.subject(owner, n.Personal)
}

func (p *piiPass) subject(owner *types.Decl, pers *ast.Personal) *types.Decl {
	if owner == nil || pers == nil || pers.Subject == nil {
		return owner
	}
	if t := pathType(owner, pers.Subject); t != nil {
		if id, ok := types.Unwrap(t).(*types.ID); ok {
			return id.Owner
		}
	}
	return owner
}

func (p *piiPass) body(m member, subjects map[*types.Decl]bool) {
	// A variable's label is the union of the labels of all its assignments
	// (flow-insensitive); iterate to a fixed point.
	for changed, i := true, 0; changed && i < 10; i++ {
		changed = false
		ast.Inspect(m.body, func(n ast.Node) bool {
			var name *ast.Ident
			var value ast.Expr
			switch s := n.(type) {
			case *ast.LetStmt:
				name, value = s.Name, s.Value
			case *ast.AssignStmt:
				name, _ = s.Target.(*ast.Ident)
				value = s.Value
			case *ast.ForStmt:
				name, value = s.Var, s.X
			}
			if name == nil {
				return true
			}
			o := p.Info.Uses[name]
			if o == nil || o.Kind != sema.ObjLocal {
				return true
			}
			l := p.label(value)
			before := len(p.locals[o.Node])
			p.locals[o.Node] = p.locals[o.Node].add(l)
			if len(p.locals[o.Node]) != before {
				changed = true
			}
			return true
		})
	}
	_, inEvent := m.node.(*ast.EventDecl)
	query, inQuery := m.node.(*ast.QueryDecl)
	cmd, inCommand := m.node.(*ast.CommandDecl)
	ast.Inspect(m.body, func(n ast.Node) bool {
		switch s := n.(type) {
		case *ast.AssignStmt:
			if !inEvent {
				return true
			}
			if id, ok := s.Target.(*ast.Ident); ok {
				if o := p.Info.Uses[id]; o != nil && o.Kind == sema.ObjField && o.Var != nil {
					if p.sink(s.Value, id.Name, o.Var.Personal, p.subjectOf(o.Var), inCommand) {
						p.flow(p.label(s.Value), FlowField, m.decl.Name, id.Name, s)
					}
				}
			}
		case *ast.ReturnStmt:
			if inQuery && s.Result != nil {
				if l := p.label(s.Result); len(l) > 0 {
					p.flow(l, FlowResult, m.decl.Name+"."+query.Name.Name, "", s)
				}
			}
		case *ast.CallExpr:
			p.call(s, inCommand, cmd)
		case *ast.ForgetStmt:
			if id, ok := types.Unwrap(p.Info.Types[s.X]).(*types.ID); ok {
				if !subjects[id.Owner] {
					p.Report(diag.W1020, rangeOf(s.X), id.Owner.Name)
				}
				p.out.Forgets = append(p.out.Forgets, Forget{In: m.decl, Member: m.node, Subject: id.Owner, Node: s})
			}
		}
		return true
	})
}

// sourceOf is the source of a personal field or parameter of self.
func (p *piiPass) sourceOf(v *types.Var) origin {
	var pers *ast.Personal
	switch n := v.Node.(type) {
	case *ast.FieldDecl:
		pers = n.Personal
	case *ast.Param:
		pers = n.Personal
	}
	s := origin{subject: p.subjectOf(v)}
	if pers != nil && pers.Subject != nil {
		s.path = pers.Subject.String()
	}
	return s
}

func (p *piiPass) subjectOf(v *types.Var) *types.Decl {
	switch v.Node.(type) {
	case *ast.FieldDecl:
		return p.fieldSubject(v)
	case *ast.Param:
		return p.paramSubject(v)
	}
	return nil
}

// call checks the call arguments that go into sinks.
func (p *piiPass) call(x *ast.CallExpr, inCommand bool, cmd *ast.CommandDecl) {
	c := p.Info.Calls[x]
	if c == nil {
		return
	}
	var params []*types.Var
	var kind FlowKind
	var target string
	if c.Decl != nil {
		target = c.Decl.Name
	}
	switch {
	case c.Kind == sema.CallMethod && c.Method.Kind == types.Event:
		params, kind, target = c.Method.Params, FlowEvent, target+"."+c.Method.Name
	case c.Kind == sema.CallRequest:
		params, kind, target = c.Method.Params, FlowRequest, target+"."+c.Method.Name
	case c.Kind == sema.CallUpsert:
		params, kind = c.Decl.Fields, FlowUpsert
	case c.Kind == sema.CallMethod && c.Method.Kind == types.Notification:
		// Notifications are not sinks: the flow is only recorded in the map.
		for _, a := range x.Args.Args {
			if l := p.label(a.Value); a.Name != nil && len(l) > 0 {
				p.flow(l, FlowNotification, target+"."+c.Method.Name, a.Name.Name, a.Value)
				p.site(x, a.Name.Name, l)
			}
		}
		return
	case c.Kind == sema.CallMethod && c.Method.Kind == types.Error:
		// Errors are not sinks either, but they go into the response by retry key.
		if x.Args == nil {
			return
		}
		for _, a := range x.Args.Args {
			if l := p.label(a.Value); a.Name != nil && len(l) > 0 {
				p.flow(l, FlowError, target+"."+c.Method.Name, a.Name.Name, a.Value)
				p.site(x, a.Name.Name, l)
			}
		}
		return
	case c.Kind == sema.CallCommand, c.Kind == sema.CallMethod && c.Method.Kind == types.Command:
		for _, a := range x.Args.Args {
			if a.Name == nil {
				continue
			}
			prm := paramByName(c.Method.Params, a.Name.Name)
			l := p.label(a.Value)
			switch {
			case prm == nil || len(l) == 0:
			case !prm.Personal:
				p.Report(diag.E0903, rangeOf(a.Value), prm.Name, c.Decl.Name+"."+c.Method.Name)
			default:
				p.flow(l, FlowCommand, target+"."+c.Method.Name, prm.Name, a.Value)
			}
		}
		return
	default:
		return
	}
	for _, a := range x.Args.Args {
		if a.Name == nil {
			continue
		}
		prm := paramByName(params, a.Name.Name)
		if prm == nil {
			continue
		}
		if p.sink(a.Value, prm.Name, prm.Personal, p.subjectOf(prm), inCommand) {
			p.flow(p.label(a.Value), kind, target, prm.Name, a.Value)
			if kind == FlowRequest {
				p.site(x, prm.Name, p.label(a.Value))
			}
		}
	}
	if kind == FlowUpsert {
		p.upsertSubjects(x, c.Decl)
	}
}

func paramByName(params []*types.Var, name string) *types.Var {
	for _, v := range params {
		if v.Name == name {
			return v
		}
	}
	return nil
}

// sink checks the flow of a value into place name. It returns true if
// personal data goes into a personal place without errors.
func (p *piiPass) sink(value ast.Expr, name string, personal bool, subject *types.Decl, inCommand bool) bool {
	l := p.label(value)
	if !personal {
		if len(l) > 0 {
			p.Report(diag.E0901, rangeOf(value), l.String(), name)
		}
		return false
	}
	if len(l) == 0 {
		if inCommand {
			p.plainParam(value, name)
		}
		return false
	}
	if subject == nil {
		return true // an external request: the subject is not set
	}
	if ds := l.subjects(); len(ds) > 1 || !ds[subject] {
		p.Report(diag.E0902, rangeOf(value), name, subject.Name, l.String())
		return false
	}
	return true
}

// plainParam: a non-personal command parameter in a personal place (W1021).
func (p *piiPass) plainParam(value ast.Expr, name string) {
	ast.Inspect(value, func(n ast.Node) bool {
		if id, ok := n.(*ast.Ident); ok {
			if o := p.Info.Uses[id]; o != nil && o.Kind == sema.ObjParam && o.Var != nil && !o.Var.Personal {
				p.Report(diag.W1021, rangeOf(id), id.Name, name)
				return false
			}
		}
		return true
	})
}

// label computes the label of an expression.
func (p *piiPass) label(x ast.Expr) label {
	var l label
	ast.Inspect(x, func(n ast.Node) bool {
		switch e := n.(type) {
		case *ast.Ident:
			o := p.Info.Uses[e]
			if o == nil {
				return true
			}
			switch o.Kind {
			case sema.ObjField, sema.ObjParam:
				if o.Var != nil && o.Var.Personal {
					l = l.add(label{p.sourceOf(o.Var): true})
				}
			case sema.ObjLocal:
				l = l.add(p.locals[o.Node])
			}
		case *ast.SelectorExpr:
			// A record field: c.phone in a query.
			if r, ok := types.Unwrap(p.Info.Types[e.X]).(*types.Record); ok {
				if f := r.Owner.Field(e.Sel.Name); f != nil && f.Personal {
					l = l.add(label{origin{subject: p.fieldSubject(f), foreign: true}: true})
				}
			}
		case *ast.CallExpr:
			// Query results and method calls are not labeled: a label comes
			// only from the arguments of pure functions and constructors.
			// schedule returns a new Timer.Id: the command arguments do
			// not go into it.
			if c := p.Info.Calls[e]; c != nil && (c.Kind == sema.CallQuery || c.Kind == sema.CallCommand ||
				c.Kind == sema.CallBuiltin && c.Builtin == "schedule") {
				return false
			}
		}
		return true
	})
	for s := range l {
		if s.subject == nil {
			delete(l, s)
		}
	}
	return l
}

// site records the subjects of argument a of call x with label l: only in
// commands and reactions, where the call goes into a unit of work.
func (p *piiPass) site(x *ast.CallExpr, arg string, l label) {
	switch p.cur.node.(type) {
	case *ast.CommandDecl, *ast.OnDecl:
	default:
		return
	}
	for s := range l {
		if s.foreign {
			continue
		}
		var path []string
		if s.path != "" {
			path = strings.Split(s.path, ".")
		}
		p.out.Sites[x] = append(p.out.Sites[x], SiteSubject{Arg: arg, Subject: s.subject, Path: path})
	}
	ss := p.out.Sites[x]
	sort.Slice(ss, func(i, j int) bool {
		a, b := ss[i], ss[j]
		if a.Arg != b.Arg {
			return a.Arg < b.Arg
		}
		if a.Subject.Name != b.Subject.Name {
			return a.Subject.Name < b.Subject.Name
		}
		return strings.Join(a.Path, ".") < strings.Join(b.Path, ".")
	})
}

// upsertSubjects: a projection operation sets a personal field but not the
// field of its subject (E0906).
func (p *piiPass) upsertSubjects(x *ast.CallExpr, d *types.Decl) {
	if x.Args == nil {
		return
	}
	passed := map[string]bool{}
	for _, a := range x.Args.Args {
		if a.Name != nil {
			passed[a.Name.Name] = true
		}
	}
	for _, a := range x.Args.Args {
		if a.Name == nil {
			continue
		}
		f := d.Field(a.Name.Name)
		if f == nil || !f.Personal {
			continue
		}
		fd, ok := f.Node.(*ast.FieldDecl)
		if !ok || fd.Personal == nil || fd.Personal.Subject == nil {
			continue
		}
		if root := fd.Personal.Subject.Elems[0].Name; root != "id" && !passed[root] {
			p.Report(diag.E0906, rangeOf(a.Value), a.Name.Name, d.Name, fd.Personal.Subject.String(), root)
		}
	}
}
