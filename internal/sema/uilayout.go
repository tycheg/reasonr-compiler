package sema

import (
	"unicode"
	"unicode/utf8"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/constant"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// The layout of screens and app roots (RFC §22.3–22.5): what each line
// means, the scopes and where values come from.

// layout checks the layout of one screen or app root.
type layout struct {
	c      *checker
	ui     *UI
	screen *Screen // nil for the app root
	apps   []*App  // the apps the screen belongs to, or the app itself
	me     *types.Decl
}

// uiValue is a named value of a scope.
type uiValue struct {
	name string
	typ  types.Type
	src  SourceKind
	node *Node
	obj  *Object
}

// uiScope is a scope: screen parameters, data, a row, a form, an event,
// primitive values.
type uiScope struct{ vals []*uiValue }

func (s *uiScope) find(name string) *uiValue {
	for _, v := range s.vals {
		if v.name == name {
			return v
		}
	}
	return nil
}

func (s *uiScope) add(name string, t types.Type, src SourceKind, node *Node, decl ast.Node, v *types.Var) {
	kind := ObjLocal
	if src == SrcParam {
		kind = ObjParam
	}
	if decl == nil {
		decl = &ast.Ident{Name: name} // parameters of an expression function differ by declaration
	}
	s.vals = append(s.vals, &uiValue{name: name, typ: t, src: src, node: node,
		obj: &Object{Kind: kind, Name: name, Type: t, Node: decl, Var: v}})
}

// fieldsScope is a scope of the fields of a value object.
func fieldsScope(t types.Type, src SourceKind, node *Node) *uiScope {
	s := &uiScope{}
	if v, ok := t.(*types.Value); ok {
		for _, f := range v.Decl.Fields {
			s.add(f.Name, f.Type, src, node, f.Node, f)
		}
	}
	return s
}

// uiCtx is where a line stands: the scopes and the meaning of lowercase names.
type uiCtx struct {
	scopes []*uiScope
	names  PartBinds
	of     types.Type // PartType: the type whose fields are the names
	role   Role
	owner  *Node // the node params and inputs refer to
	where  any   // where the lines stand: a string or a diag.Message for messages
}

func (x *uiCtx) with(s *uiScope) *uiCtx {
	y := *x
	y.scopes = append(append([]*uiScope(nil), x.scopes...), s)
	return &y
}

// only is the same scopes, but lowercase names are not allowed.
func (x *uiCtx) only(where any) *uiCtx {
	y := *x
	y.names, y.of, y.owner, y.where = PartNone, nil, nil, where
	return &y
}

func (x *uiCtx) lookup(name string) *uiValue {
	for i := len(x.scopes) - 1; i >= 0; i-- {
		if v := x.scopes[i].find(name); v != nil {
			return v
		}
	}
	return nil
}

// body is the body for layout expressions: scope values are visible as variables.
func (l *layout) body(x *uiCtx) *body {
	b := l.c.newBody(ctxUI, nil, nil)
	if l.me != nil {
		b.me = l.me.ID
	}
	for _, s := range x.scopes {
		b.push()
		for _, v := range s.vals {
			b.declare(v.obj)
		}
	}
	return b
}

// source returns the scope value expression x (a bare name) refers to, or nil.
func (l *layout) valueOf(x *uiCtx, e ast.Expr) *uiValue {
	id, ok := e.(*ast.Ident)
	if !ok {
		return nil
	}
	o := l.c.info.Uses[id]
	for _, s := range x.scopes {
		for _, v := range s.vals {
			if v.obj == o {
				return v
			}
		}
	}
	return nil
}

var (
	textUIRoot       = "the layout root holds only primitives and screens"
	textUIOnlyPrims  = "part %s holds only primitives"
	textUIUnderPrim  = "%s holds only primitives and parts"
	textUIScreensOf  = "items of %s are screens"
	textUIUnknown    = "no primitive, part or screen %q"
	textUIPartExtra  = "part %s takes no parameters, binding or condition"
	textUINoBinding  = "%s is a layout: it takes no binding"
	textUIOnField    = "%s shows a field: write the field name before the primitive — 'price %s'"
	textUIOnInput    = "%s is an input: write the parameter name before the primitive — 'reason %s'"
	textUINeedsQuery = "%s is placed on a query: %s Type.Query"
	textUINeedsCmd   = "%s is placed on a command: %s Type.Command"
	textUINeedsScr   = "%s is placed on a screen: %s Screen"
	textUIResult     = "%s binds a query returning %s, but %s returns %s"
	textUIScreenBind = "screen %s takes no binding: link to it with Link %s"
	textUIScreenKids = "embedded screen %s has no lines of its own: its layout is in the screen declaration"
	textUINoType     = "no entity, actor or projection %q"
	textUINoMethod   = "%s has no method %q"
	textUIPrivate    = "%s is a private method: only public commands and queries are available in the UI"
	textUINotQuery   = "%s is a command, but %s is placed on a query"
	textUINotCommand = "%s is a query, but %s is placed on a command"
	textUINoScreen   = "no screen %q"
	textUITargetType = "the target of [x].Command is an entity or actor id"
	textUINoField    = "%s has no field %q"
	textUINoParam    = "%s has no parameter %q"
	textUIBound      = "argument %q of %s is already bound (%s): it has no input"
	textUIFieldHere  = "a field line %q is not allowed here: %s"
	textUIShowPrim   = "%s is an input, but field %q is shown here"
	textUIInputPrim  = "%s does not take input, but %q is an input here"
	textUIFieldType  = "%s takes %s, but %q is %s"
	textUINoEvent    = "%s has no event %q"
	textUIOnOutside  = "on is a transition on a primitive event: put the line under the primitive"
	textUIValueType  = "%q for %s comes from the scope (%s) with type %s, but %s is needed: bind the argument explicitly"
	textUINewNotID   = "new is a new id, but %s is expected here"
	textUIAmbiguous  = "the target of %s is ambiguous: the scope has several %s — %s; set the target explicitly: [%s].%s"
	textUIMeApps     = "screen %s belongs to apps with different callers: in %s the caller is not %s; set the target of %s explicitly"
	textUIScreenArgs = "screen %s needs parameters %s that cannot be taken from anywhere; pass them: %s(%s: …)"
)

// nodes checks layout lines in one context.
func (l *layout) nodes(x *uiCtx, list []ast.UINode, parent *Node) []*Node {
	var out []*Node
	for _, n := range list {
		if node := l.node(x, n, parent); node != nil {
			out = append(out, node)
		}
	}
	return out
}

func (l *layout) node(x *uiCtx, n ast.UINode, parent *Node) *Node {
	var node *Node
	switch n := n.(type) {
	case *ast.PrimNode:
		node = l.prim(x, n, parent)
	case *ast.FieldNode:
		node = l.field(x, n, parent)
	case *ast.OnNode:
		node = l.on(x, n, parent)
	}
	if node != nil {
		l.ui.Nodes[n] = node
	}
	return node
}

func (l *layout) prim(x *uiCtx, n *ast.PrimNode, parent *Node) *Node {
	name := n.Name.Name
	if parent != nil && parent.Prim != nil && parent.Kind != NodePart {
		if pt := parent.Prim.Part(name); pt != nil {
			return l.part(x, n, parent, pt)
		}
	}
	if p := l.ui.prims[name]; p != nil {
		return l.primitive(x, n, parent, p)
	}
	if s := l.ui.find(n.Name, name); s != nil {
		return l.screenRef(x, n, parent, s)
	}
	d := l.c.bag.Add(diag.E0704, rangeOf(n.Name), diag.Message{Format: textUIUnknown, Args: []any{name}})
	var cands []string
	for _, p := range l.ui.Primitives {
		cands = append(cands, p.Name)
	}
	for _, s := range l.ui.Screens {
		cands = append(cands, s.Name)
	}
	if parent != nil && parent.Prim != nil {
		for _, pt := range parent.Prim.Parts {
			cands = append(cands, pt.Name)
		}
	}
	if s := closest(name, cands); s != "" {
		d.Hint(hintDidYouMean, s)
	}
	return nil
}

func (l *layout) newNode(kind NodeKind, n ast.UINode, parent *Node) *Node {
	return &Node{Kind: kind, AST: n, Screen: l.screen, Parent: parent}
}

func (l *layout) part(x *uiCtx, n *ast.PrimNode, parent *Node, pt *Part) *Node {
	node := l.newNode(NodePart, n, parent)
	node.Part = pt
	if n.Args != nil || n.Binding != nil || n.Cond != nil {
		l.c.bag.Add(diag.E0702, rangeOf(n.Name), diag.Message{Format: textUIPartExtra, Args: []any{pt.Name}})
	}
	if n.Label != nil {
		node.Label = n.Label.Value
	}
	node.Children = l.nodes(l.partCtx(x, parent, pt), n.Children, node)
	return node
}

func (l *layout) screenRef(x *uiCtx, n *ast.PrimNode, parent *Node, s *Screen) *Node {
	node := l.newNode(NodeScreen, n, parent)
	node.To = s
	switch {
	case n.Binding != nil:
		l.c.bag.Add(diag.E0702, rangeOf(n.Binding), diag.Message{Format: textUIScreenBind, Args: []any{s.Name, s.Name}})
	case len(n.Children) > 0:
		l.c.bag.Add(diag.E0704, rangeOf(n.Name), diag.Message{Format: textUIScreenKids, Args: []any{s.Name}})
	}
	node.ToArgs = l.screenArgs(x, s, n.Args, n.Name)
	l.cond(x, node, n.Cond)
	if n.Label != nil {
		node.Label = n.Label.Value
	}
	return node
}

func (l *layout) primitive(x *uiCtx, n *ast.PrimNode, parent *Node, p *Primitive) *Node {
	node := l.newNode(NodePrimitive, n, parent)
	node.Prim = p
	s := subst{}
	bind := n.Binding
	at := rangeOf(n.Name)
	bound := false // the binding set the type variables
	switch p.Binds {
	case BindNone:
		bound = true
		if bind != nil {
			l.c.bag.Add(diag.E0702, rangeOf(bind), diag.Message{Format: textUINoBinding, Args: []any{p.Name}})
		}
	case BindField:
		l.c.bag.Add(diag.E0702, at, diag.Message{Format: textUIOnField, Args: []any{p.Name, p.Name}})
	case BindInput:
		l.c.bag.Add(diag.E0702, at, diag.Message{Format: textUIOnInput, Args: []any{p.Name, p.Name}})
	case BindScreen:
		bound = true
		if bind == nil {
			l.c.bag.Add(diag.E0702, at, diag.Message{Format: textUINeedsScr, Args: []any{p.Name, p.Name}})
		} else {
			l.bindScreen(x, node, bind)
		}
	case BindQuery:
		if bind == nil {
			l.c.bag.Add(diag.E0702, at, diag.Message{Format: textUINeedsQuery, Args: []any{p.Name, p.Name}})
		} else {
			bound = l.bindQuery(x, node, bind, s)
		}
	case BindCommand:
		bound = true
		if bind == nil {
			l.c.bag.Add(diag.E0702, at, diag.Message{Format: textUINeedsCmd, Args: []any{p.Name, p.Name}})
		} else {
			l.bindCommand(x, node, bind)
		}
	}
	l.primArgs(x, node, n.Args, s, n.Name, bound)
	l.cond(x, node, n.Cond)
	if n.Label != nil {
		node.Label = n.Label.Value
	}
	l.children(x, node, n.Children)
	return node
}

func (l *layout) cond(x *uiCtx, node *Node, cond ast.Expr) {
	if cond == nil {
		return
	}
	b := l.body(x)
	b.cond(cond)
	node.Cond = l.newExpr(x, cond, tBool)
	// Layout flags (RFC §23.4): one per condition, with no
	// flags in the conditions of ancestors.
	if flag := b.condFlag(b.flagReads); flag != nil {
		for p := node.Parent; p != nil; p = p.Parent {
			if p.Cond == nil {
				continue
			}
			if outer := l.c.info.FeaturesIn(p.Cond.X); len(outer) > 0 {
				l.c.bag.Add(diag.E1502, rangeOf(flag), l.c.info.Uses[flag].Name, outer[0].Name)
				break
			}
		}
	}
}

// newExpr assembles an expression already checked in context x: the scope
// values it reads and the constant value.
func (l *layout) newExpr(x *uiCtx, e ast.Expr, t types.Type) *Expr {
	out := &Expr{X: e, Type: t}
	seen := map[*Object]bool{}
	ast.Inspect(e, func(n ast.Node) bool {
		id, ok := n.(*ast.Ident)
		if !ok {
			return true
		}
		o := l.c.info.Uses[id]
		if o == nil || seen[o] {
			return true
		}
		for _, s := range x.scopes {
			for _, v := range s.vals {
				if v.obj == o {
					seen[o] = true
					out.Uses = append(out.Uses, &Arg{Name: v.name, Type: v.typ, Source: v.src, From: v.node, Field: v.name})
					out.Vars = append(out.Vars, &types.Var{Name: v.name, Node: o.Node, Type: v.typ})
				}
			}
		}
		return true
	})
	if len(out.Uses) == 0 {
		out.Const, out.IsConst = constant.Eval(e, &constEnv{c: l.c})
	}
	return out
}

// method resolves Type.Method of a binding: a public method of an entity,
// an actor or a projection.
func (l *layout) method(typ, name *ast.Ident) (*types.Decl, *types.Method) {
	d := l.c.lookup(typ, typ.Name)
	if d == nil || !d.Kind.HasID() {
		l.c.bag.Add(diag.E0703, rangeOf(typ), diag.Message{Format: textUINoType, Args: []any{typ.Name}})
		return nil, nil
	}
	return l.methodOf(d, name)
}

// methodOf resolves a public method of an entity or a projection.
func (l *layout) methodOf(d *types.Decl, name *ast.Ident) (*types.Decl, *types.Method) {
	m := d.Method(name.Name)
	if m == nil {
		diagnostic := l.c.bag.Add(diag.E0703, rangeOf(name), diag.Message{Format: textUINoMethod, Args: []any{d.Name, name.Name}})
		var cands []string
		for _, m := range d.Methods {
			if m.Public() {
				cands = append(cands, m.Name)
			}
		}
		if s := closest(name.Name, cands); s != "" {
			diagnostic.Hint(hintDidYouMean, s)
		}
		return d, nil
	}
	if !m.Public() {
		l.c.bag.Add(diag.E0703, rangeOf(name), diag.Message{Format: textUIPrivate, Args: []any{d.Short + "." + m.Name}})
		return d, nil
	}
	return d, m
}

func (l *layout) bindScreen(x *uiCtx, node *Node, b *ast.Binding) {
	if !b.IsScreen() || b.Target != nil {
		l.c.bag.Add(diag.E0703, rangeOf(b), diag.Message{Format: textUINeedsScr, Args: []any{node.Prim.Name, node.Prim.Name}})
		return
	}
	s := l.ui.find(b.Type, b.Type.Name)
	if s == nil {
		l.c.bag.Add(diag.E0703, rangeOf(b.Type), diag.Message{Format: textUINoScreen, Args: []any{b.Type.Name}})
		return
	}
	node.To = s
	node.ToArgs = l.screenArgs(x, s, b.Args, b.Type)
}

// bindQuery binds a primitive to a query; false means the primitive's type
// variables were not inferred because of an error.
func (l *layout) bindQuery(x *uiCtx, node *Node, b *ast.Binding, s subst) bool {
	p := node.Prim
	if b.Target != nil || b.Method == nil {
		l.c.bag.Add(diag.E0703, rangeOf(b), diag.Message{Format: textUINeedsQuery, Args: []any{p.Name, p.Name}})
		return false
	}
	owner, m := l.method(b.Type, b.Method)
	if m == nil {
		return false
	}
	if m.Kind != types.Query {
		l.c.bag.Add(diag.E0703, rangeOf(b.Method), diag.Message{Format: textUINotQuery, Args: []any{owner.Name + "." + m.Name, p.Name}})
		return false
	}
	ok := unify(p.BindType, m.Result, s)
	if !ok {
		l.c.bag.Add(diag.E0702, rangeOf(b), diag.Message{Format: textUIResult,
			Args: []any{p.Name, p.BindType.String(), owner.Name + "." + m.Name, m.Result.String()}})
	}
	node.Owner, node.Method = owner, m
	node.Args = l.methodArgs(x, node, m, b.Args, nil, b)
	return ok
}

func (l *layout) bindCommand(x *uiCtx, node *Node, b *ast.Binding) {
	p := node.Prim
	if b.Method == nil {
		l.c.bag.Add(diag.E0703, rangeOf(b), diag.Message{Format: textUINeedsCmd, Args: []any{p.Name, p.Name}})
		return
	}
	var owner *types.Decl
	var m *types.Method
	var target *Arg
	if b.Target != nil {
		e := b.Target.Elems[0]
		t := l.body(x).expr(e, nil)
		id, ok := t.(*types.ID)
		if !ok || id.Owner.Kind == types.ProjectionDecl {
			if !types.IsInvalid(t) {
				l.c.bag.Add(diag.E0703, rangeOf(e), textUITargetType)
			}
			return
		}
		if owner, m = l.methodOf(id.Owner, b.Method); m == nil {
			return
		}
		target = l.exprArg(x, lowerFirst(owner.Short), id, e)
	} else {
		if owner, m = l.method(b.Type, b.Method); m == nil {
			return
		}
	}
	if m.Kind != types.Command {
		l.c.bag.Add(diag.E0703, rangeOf(b.Method), diag.Message{Format: textUINotCommand, Args: []any{owner.Name + "." + m.Name, p.Name}})
		return
	}
	node.Owner, node.Method = owner, m
	if target == nil && !isCreate(m) { // a creation command has no target: the engine issues the id
		target = l.target(x, node, b)
	}
	node.Target = target
	node.Args = l.methodArgs(x, node, m, b.Args, nil, b)
}

// exprArg is a value set by an expression: a bare name takes the source of
// the scope value, new is a new identifier.
func (l *layout) exprArg(x *uiCtx, name string, t types.Type, e ast.Expr) *Arg {
	if nw, ok := e.(*ast.NewIDExpr); ok {
		_ = nw
		return &Arg{Name: name, Type: t, Source: SrcNew, Expr: e}
	}
	if v := l.valueOf(x, e); v != nil {
		return &Arg{Name: name, Type: t, Source: v.src, From: v.node, Field: v.name, Expr: e}
	}
	return &Arg{Name: name, Type: t, Source: SrcExpr, Expr: e, Code: l.newExpr(x, e, t)}
}

// target finds the target of a command without an explicit [x] (RFC §22.4, item 2).
func (l *layout) target(x *uiCtx, node *Node, b *ast.Binding) *Arg {
	owner, m := node.Owner, node.Method
	name := lowerFirst(owner.Short)
	for i := len(x.scopes) - 1; i >= 0; i-- {
		var found []*uiValue
		for _, v := range x.scopes[i].vals {
			if identical(types.Unwrap(v.typ), owner.ID) {
				found = append(found, v)
			}
		}
		switch {
		case len(found) == 1:
			v := found[0]
			return &Arg{Name: name, Type: owner.ID, Source: v.src, From: v.node, Field: v.name}
		case len(found) > 1:
			var names []string
			for _, v := range found {
				names = append(names, v.name)
			}
			l.c.bag.Add(diag.E0705, rangeOf(b), diag.Message{Format: textUIAmbiguous,
				Args: []any{owner.Name + "." + m.Name, owner.ID.String(), joinTypes(names), found[0].name, m.Name}})
			return nil
		}
	}
	if owner.Kind == types.ActorDecl && len(l.apps) > 0 {
		var other *App
		for _, a := range l.apps {
			if a.Me == nil || !sameEntity(a.Me, owner) {
				other = a
			}
		}
		if other == nil {
			return &Arg{Name: name, Type: owner.ID, Source: SrcMe}
		}
		if len(l.apps) > 1 && l.anyMe(owner) {
			l.c.bag.Add(diag.E0711, rangeOf(b), diag.Message{Format: textUIMeApps,
				Args: []any{l.screen.Name, other.Name, owner.Name, owner.Name + "." + m.Name}})
			return &Arg{Name: name, Type: owner.ID, Source: SrcMe}
		}
	}
	return &Arg{Name: name, Type: owner.ID, Source: SrcInput, Input: true}
}

func (l *layout) anyMe(d *types.Decl) bool {
	for _, a := range l.apps {
		if a.Me != nil && sameEntity(a.Me, d) {
			return true
		}
	}
	return false
}

// methodArgs binds the arguments of a query or command (RFC §22.4, item 1):
// explicit ones, the primitive's values (provided), same-named scope
// values, PageRequest from the primitive, the rest as input.
func (l *layout) methodArgs(x *uiCtx, node *Node, m *types.Method, args *ast.ArgList, provided *uiScope, at ast.Node) []*Arg {
	ex := x
	if provided != nil {
		ex = x.with(provided)
	}
	what := m.Name
	if node != nil && node.Owner != nil {
		what = node.Owner.Name + "." + m.Name
	}
	explicit := l.explicitArgs(ex, args, what, m.Params)
	var out []*Arg
	for _, p := range m.Params {
		if a := explicit[p.Name]; a != nil {
			a.Input = m.Kind == types.Command && !a.Source.Fixed()
			a.Param = p
			out = append(out, a)
			continue
		}
		arg := &Arg{Name: p.Name, Type: p.Type, Param: p}
		v := (*uiValue)(nil)
		if provided != nil {
			v = provided.find(p.Name)
		}
		if v == nil {
			v = x.lookup(p.Name)
		}
		switch {
		case v != nil:
			if !assignableUI(v.typ, p.Type) {
				l.c.bag.Add(diag.E0706, rangeOf(at), diag.Message{Format: textUIValueType,
					Args: []any{p.Name, what, v.src.String(), v.typ.String(), p.Type.String()}})
			}
			arg.Source, arg.From, arg.Field = v.src, v.node, v.name
		case isPageReq(p.Type) && m.Kind == types.Query:
			arg.Source = SrcPrimitive
		default:
			arg.Source = SrcInput
			arg.Default = !required(p)
		}
		arg.Input = arg.Source == SrcInput || m.Kind == types.Command && !arg.Source.Fixed()
		out = append(out, arg)
	}
	return out
}

// explicitArgs checks the arguments set in a binding. A bare name of a
// scope value from data is the initial input value: a field T? fits a
// parameter T (RFC §22.4, item 3).
func (l *layout) explicitArgs(x *uiCtx, args *ast.ArgList, what string, params []*types.Var) map[string]*Arg {
	explicit := map[string]*Arg{}
	if args == nil {
		return explicit
	}
	byName := map[string]*types.Var{}
	var names []string
	for _, p := range params {
		byName[p.Name] = p
		names = append(names, p.Name)
	}
	b := l.body(x)
	for _, a := range args.Args {
		if a.Name == nil {
			b.expr(a.Value, nil)
			l.c.bag.Add(diag.E0010, rangeOf(a.Value), quoteName(what))
			continue
		}
		p := byName[a.Name.Name]
		switch {
		case p == nil:
			b.expr(a.Value, nil)
			d := l.c.bag.Add(diag.E0204, rangeOf(a.Name), quoteName(what), a.Name.Name)
			if s := closest(a.Name.Name, names); s != "" {
				d.Hint(hintDidYouMean, s)
			}
			continue
		case explicit[p.Name] != nil:
			l.c.bag.Add(diag.E0205, rangeOf(a.Name), a.Name.Name)
			continue
		}
		if id, ok := a.Value.(*ast.Ident); ok {
			if v := x.lookup(id.Name); v != nil && !v.src.Fixed() {
				l.c.info.Uses[id] = v.obj
				l.c.info.Types[id] = v.typ
				if !assignableUI(v.typ, p.Type) {
					l.c.bag.Add(diag.E0706, rangeOf(id), diag.Message{Format: textUIValueType,
						Args: []any{p.Name, what, v.src.String(), v.typ.String(), p.Type.String()}})
				}
				explicit[p.Name] = &Arg{Name: p.Name, Type: p.Type, Source: v.src, From: v.node, Field: v.name, Expr: a.Value}
				continue
			}
		}
		b.check(a.Value, p.Type)
		explicit[p.Name] = l.exprArg(x, p.Name, p.Type, a.Value)
	}
	return explicit
}

func isPageReq(t types.Type) bool {
	_, ok := t.(*types.PageReq)
	return ok
}

// assignableUI: a scope value fits a parameter; a value T? is the initial
// value of an input T.
func assignableUI(from, to types.Type) bool {
	return assignable(from, to) || assignable(types.Unwrap(from), to)
}

// screenArgs fills in the screen parameters: explicit arguments, then
// same-named scope values (RFC §22.5).
func (l *layout) screenArgs(x *uiCtx, s *Screen, args *ast.ArgList, at ast.Node) []*Arg {
	explicit := map[string]*Arg{}
	if args != nil {
		b := l.body(x)
		b.partialArgs(at, args, quoteName(s.Name), s.Params)
		for _, a := range args.Args {
			if a.Name == nil {
				continue
			}
			for _, p := range s.Params {
				if p.Name == a.Name.Name && explicit[p.Name] == nil {
					explicit[p.Name] = l.exprArg(x, p.Name, p.Type, a.Value)
				}
			}
		}
	}
	var out []*Arg
	var missing []string
	for _, p := range s.Params {
		if a := explicit[p.Name]; a != nil {
			out = append(out, a)
			continue
		}
		v := x.lookup(p.Name)
		if v == nil {
			if required(p) {
				missing = append(missing, p.Name)
			}
			continue
		}
		if !assignable(v.typ, p.Type) {
			l.c.bag.Add(diag.E0706, rangeOf(at), diag.Message{Format: textUIValueType,
				Args: []any{p.Name, s.Name, v.src.String(), v.typ.String(), p.Type.String()}})
		}
		out = append(out, &Arg{Name: p.Name, Type: p.Type, Source: v.src, From: v.node, Field: v.name})
	}
	if len(missing) > 0 {
		l.c.bag.Add(diag.E0710, rangeOf(at), diag.Message{Format: textUIScreenArgs,
			Args: []any{s.Name, joinTypes(missing), s.Name, missing[0]}})
	}
	return out
}

func lowerFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToLower(r)) + s[n:]
}

// isCreate reports that a command is a creation command (command … create Name).
func isCreate(m *types.Method) bool {
	c, ok := m.Node.(*ast.CommandDecl)
	return ok && c.Create.IsValid()
}
