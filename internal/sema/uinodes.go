package sema

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// Children and parts of primitives, fields, transitions, primitive parameters (RFC §22.7).

var (
	textUINoShow       = "no display primitive for type %s: declare default %s Primitive"
	textUINoInput      = "no input primitive for %q of type %s: declare default %s Primitive"
	textUINoInputID    = "%q of type %s is entered by picking: declare default %s EntityPicker(search: …) or pass the value in the binding"
	textUINoInputPlace = "%s has no inputs: %q cannot be taken from anywhere — pass the value in the binding"
	textUINotShown     = "input %q is not shown: add the line %s or pass the value in the binding"
	textUIPrimParam    = "%s has no parameter %q"
	textUIPrimMissing  = "parameter %q of primitive %s is not passed"
	textUIPrimQuery    = "parameter %q is a query: Type.Query(arguments)"
	textUIPrimResult   = "parameter %q needs a query returning %s, but %s returns %s"
	textUIPrimField    = "parameter %q is a field name of row %s"
	textUIPrimFieldTyp = "field %q has type %s, but parameter %q needs a field of type %s"
	textUIAutoNone     = "%s has no field of type %s for %q: set it explicitly"
	textUIAutoMany     = "%s has several fields of type %s for %q: %s — set one explicitly"
	textUIAutoLabel    = "%s has no string field for %q: set it explicitly"
	textUIPrimScreen   = "parameter %q is a screen"
	textUIUnbound      = "cannot infer %s of primitive %s: pass a parameter or binding that sets it"
	textUISearchArg    = "argument %q of query %s is not bound: pass it — %s: %s"
)

// children checks the children and parts of a primitive and marks what the
// automatic UI will fill.
func (l *layout) children(x *uiCtx, node *Node, list []ast.UINode) {
	p := node.Prim
	base := x.only(diag.Message{Format: textUIUnderPrim, Args: []any{p.Name}})
	if m := node.Method; m != nil {
		switch {
		case m.Kind == types.Query:
			if v, ok := m.Result.(*types.Value); ok {
				base = base.with(fieldsScope(v, SrcData, node))
			}
		case m.Kind == types.Command:
			form := &uiScope{}
			for _, a := range node.allArgs() {
				form.add(a.Name, a.Type, SrcForm, node, nil, nil)
			}
			base = base.with(form)
		}
	}
	kids := base
	if p.Children != nil {
		kids = l.partCtx(base, node, p.Children)
	}
	written := map[*Part]bool{}
	shown := map[string]bool{}
	kidsWritten := false
	for _, n := range list {
		if pn, ok := n.(*ast.PrimNode); ok && p.Part(pn.Name.Name) != nil {
			child := l.node(base, n, node)
			if child != nil {
				written[child.Part] = true
				node.Children = append(node.Children, child)
				markShown(child.Children, shown)
			}
			continue
		}
		if _, on := n.(*ast.OnNode); !on {
			kidsWritten = true
		}
		if child := l.node(kids, n, node); child != nil {
			node.Children = append(node.Children, child)
			if child.Kind == NodeField {
				shown[child.Field] = true
			}
		}
	}
	for _, pt := range p.Parts {
		if !written[pt] && p.AutoPart(pt) && node.hasAuto(pt) {
			node.Auto = append(node.Auto, pt.Name)
			l.autoFields(node, pt)
		}
	}
	if p.Children != nil && p.AutoPart(p.Children) && !kidsWritten && node.hasAuto(p.Children) {
		node.Auto = append(node.Auto, "children")
		l.autoFields(node, p.Children)
	}
	l.coverInputs(node, written, kidsWritten, shown)
}

// hasAuto reports whether there is something to fill an omitted part with:
// the method's inputs for params and inputs; always for fields.
func (n *Node) hasAuto(pt *Part) bool {
	if pt.Binds != PartParams && pt.Binds != PartInputs {
		return true
	}
	for _, a := range n.allArgs() {
		if a.Input {
			return true
		}
	}
	return false
}

// allArgs are the command arguments and the target.
func (n *Node) allArgs() []*Arg {
	if n.Target == nil {
		return n.Args
	}
	return append(append([]*Arg(nil), n.Args...), n.Target)
}

func markShown(nodes []*Node, shown map[string]bool) {
	for _, n := range nodes {
		if n.Kind == NodeField {
			shown[n.Field] = true
		}
	}
}

// AutoPart reports whether the automatic UI fills an omitted part (RFC
// §22.7): all arguments not passed (params), all inputs (inputs), all
// fields — in the first part of the primitive that binds fields of this
// type (children before parts). Later parts with the same type (RowActions
// after Columns) give only the row scope and are empty without lines.
func (p *Primitive) AutoPart(pt *Part) bool {
	switch pt.Binds {
	case PartParams, PartInputs:
		return true
	case PartType:
		for _, other := range append([]*Part{p.Children}, p.Parts...) {
			if other == nil || other.Binds != PartType {
				continue
			}
			if identicalUI(other.Type, pt.Type) {
				return other == pt
			}
		}
	}
	return false
}

// partCtx is the context of the lines of a part: what names mean and the row scope.
func (l *layout) partCtx(x *uiCtx, node *Node, pt *Part) *uiCtx {
	name := pt.Name
	if name == "" {
		name = node.Prim.Name
	}
	y := x.only(diag.Message{Format: textUIOnlyPrims, Args: []any{name}})
	y.owner = node
	y.names = pt.Binds
	switch pt.Binds {
	case PartScreens:
		y.where = diag.Message{Format: textUIScreensOf, Args: []any{name}}
	case PartType:
		t := apply(pt.Type, node.subst())
		y.of = t
		if node.Prim.Binds == BindInput {
			y.role = RoleInput
		}
		if node.Method != nil && node.Method.Kind == types.Query {
			if e := elemOf(node.Method.Result); e != nil && identical(e, t) {
				y = y.with(fieldsScope(t, SrcRow, node))
			}
		}
	}
	return y
}

// autoFields checks that an automatically filled part with fields has a
// default primitive for every field.
func (l *layout) autoFields(node *Node, pt *Part) {
	if pt.Binds != PartType {
		return
	}
	t, ok := apply(pt.Type, node.subst()).(*types.Value)
	if !ok {
		return
	}
	role := RoleShow
	if node.Prim.Binds == BindInput {
		role = RoleInput
	}
	for _, f := range t.Decl.Fields {
		if l.defaultFor(f.Type, role) == nil {
			l.noDefault(node.AST, f.Name, f.Type, role)
			return
		}
	}
}

func (l *layout) noDefault(at ast.Node, name string, t types.Type, role Role) {
	u := types.Unwrap(t)
	switch {
	case role == RoleShow:
		l.c.bag.Add(diag.E0707, nodeRange(at), diag.Message{Format: textUINoShow, Args: []any{u.String(), u.String()}})
	case isID(u):
		l.c.bag.Add(diag.E0707, nodeRange(at), diag.Message{Format: textUINoInputID, Args: []any{name, u.String(), u.String()}})
	default:
		l.c.bag.Add(diag.E0707, nodeRange(at), diag.Message{Format: textUINoInput, Args: []any{name, u.String(), u.String()}})
	}
}

func isID(t types.Type) bool {
	switch t.(type) {
	case *types.ID, *types.TimerID:
		return true
	}
	return false
}

// nodeRange is the heading of a layout line: the binding, or the name without one.
func nodeRange(n ast.Node) source.Range {
	switch n := n.(type) {
	case *ast.PrimNode:
		if n.Binding != nil {
			return rangeOf(n.Binding)
		}
		return rangeOf(n.Name)
	case *ast.FieldNode:
		return rangeOf(n.Name)
	}
	return rangeOf(n)
}

// coverInputs checks the inputs of a command or query: each has a place and
// a primitive (RFC §22.4, item 1.3).
func (l *layout) coverInputs(node *Node, written map[*Part]bool, kidsWritten bool, shown map[string]bool) {
	m := node.Method
	if m == nil {
		return
	}
	want := PartInputs
	if m.Kind == types.Query {
		want = PartParams
	}
	var place *Part
	placeWritten := false
	if pt := node.Prim.Children; pt != nil && pt.Binds == want {
		place, placeWritten = pt, kidsWritten
	}
	for _, pt := range node.Prim.Parts {
		if place == nil && pt.Binds == want {
			place, placeWritten = pt, written[pt]
		}
	}
	for _, a := range node.allArgs() {
		if !a.Input || shown[a.Name] {
			continue
		}
		req := a.Source == SrcInput && !a.Default && !isOptional(a.Type)
		switch {
		case place == nil:
			if req {
				l.c.bag.Add(diag.E0708, nodeRange(node.AST), diag.Message{Format: textUINoInputPlace, Args: []any{node.Prim.Name, a.Name}})
			}
		case placeWritten:
			if req {
				l.c.bag.Add(diag.E0708, nodeRange(node.AST), diag.Message{Format: textUINotShown, Args: []any{a.Name, a.Name}})
			}
		default:
			if l.defaultFor(a.Type, RoleInput) == nil {
				l.noDefault(node.AST, a.Name, a.Type, RoleInput)
			}
		}
	}
}

// field checks a field line: displaying a data field, a query filter or a
// command input; the part sets the meaning.
func (l *layout) field(x *uiCtx, n *ast.FieldNode, parent *Node) *Node {
	node := l.newNode(NodeField, n, parent)
	name := n.Name.Name
	node.Field = name
	node.Role = x.role
	switch x.names {
	case PartType:
		v, ok := x.of.(*types.Value)
		if !ok {
			return nil
		}
		f := v.Decl.Field(name)
		if f == nil {
			d := l.c.bag.Add(diag.E0704, rangeOf(n.Name), diag.Message{Format: textUINoField, Args: []any{v.Decl.Name, name}})
			var cands []string
			for _, f := range v.Decl.Fields {
				cands = append(cands, f.Name)
			}
			if s := closest(name, cands); s != "" {
				d.Hint(hintDidYouMean, s)
			}
			return nil
		}
		node.FieldType = f.Type
		node.TextKey = v.Decl.Name + "." + name
	case PartParams, PartInputs:
		owner := x.owner
		if owner.Method == nil {
			return nil
		}
		what := owner.Owner.Name + "." + owner.Method.Name
		var arg *Arg
		for _, a := range owner.allArgs() {
			if a.Name == name {
				arg = a
			}
		}
		switch {
		case arg == nil:
			d := l.c.bag.Add(diag.E0704, rangeOf(n.Name), diag.Message{Format: textUINoParam, Args: []any{what, name}})
			var cands []string
			for _, a := range owner.allArgs() {
				if a.Input {
					cands = append(cands, a.Name)
				}
			}
			if s := closest(name, cands); s != "" {
				d.Hint(hintDidYouMean, s)
			}
			return nil
		case !arg.Input:
			l.c.bag.Add(diag.E0704, rangeOf(n.Name), diag.Message{Format: textUIBound, Args: []any{name, what, arg.Source.String()}})
			return nil
		}
		node.FieldType = arg.Type
		node.Role = RoleInput
		node.TextKey = what + "." + name
	default:
		l.c.bag.Add(diag.E0704, rangeOf(n.Name), diag.Message{Format: textUIFieldHere, Args: []any{name, x.where}})
		return nil
	}
	s := subst{}
	if n.Prim != nil {
		p := l.ui.prims[n.Prim.Name]
		if p == nil {
			l.c.bag.Add(diag.E0704, rangeOf(n.Prim), diag.Message{Format: textUIUnknown, Args: []any{n.Prim.Name}})
			return node
		}
		node.Prim = p
		want := BindField
		if node.Role == RoleInput {
			want = BindInput
		}
		switch {
		case p.Binds != want && node.Role == RoleShow:
			l.c.bag.Add(diag.E0702, rangeOf(n.Prim), diag.Message{Format: textUIShowPrim, Args: []any{p.Name, name}})
			return node
		case p.Binds != want:
			l.c.bag.Add(diag.E0702, rangeOf(n.Prim), diag.Message{Format: textUIInputPrim, Args: []any{p.Name, name}})
			return node
		case !unifyValue(p.BindType, node.FieldType, s):
			l.c.bag.Add(diag.E0702, rangeOf(n.Prim), diag.Message{Format: textUIFieldType,
				Args: []any{p.Name, p.BindType.String(), name, node.FieldType.String()}})
			return node
		}
		l.primArgs(x, node, n.PrimArgs, s, n.Prim, true)
	} else {
		d := l.defaultFor(node.FieldType, node.Role)
		if d == nil {
			l.noDefault(n, name, node.FieldType, node.Role)
			return node
		}
		node.Prim, node.Default, node.Params = d.Prim, d, d.Params
		unifyValue(d.Prim.BindType, node.FieldType, s)
		node.s = s
		node.Subst = s.names()
	}
	l.cond(x, node, n.Cond)
	if n.Label != nil {
		node.Label = n.Label.Value
	}
	l.children(x, node, n.Children)
	return node
}

// on checks a transition on an event of the parent primitive.
func (l *layout) on(x *uiCtx, n *ast.OnNode, parent *Node) *Node {
	if parent == nil || parent.Prim == nil || parent.Kind == NodePart {
		l.c.bag.Add(diag.E0709, rangeOf(n), textUIOnOutside)
		return nil
	}
	p := parent.Prim
	ev := p.Event(n.Event.Name)
	if ev == nil {
		d := l.c.bag.Add(diag.E0709, rangeOf(n.Event), diag.Message{Format: textUINoEvent, Args: []any{p.Name, n.Event.Name}})
		var cands []string
		for _, e := range p.Events {
			cands = append(cands, e.Name)
		}
		if s := closest(n.Event.Name, cands); s != "" {
			d.Hint(hintDidYouMean, s)
		}
		return nil
	}
	node := l.newNode(NodeOn, n, parent)
	node.Event = ev
	data := &uiScope{}
	if ev.Name == "done" && p.Binds == BindCommand {
		for _, a := range parent.allArgs() {
			data.add(a.Name, a.Type, SrcEvent, parent, nil, nil)
		}
	}
	for _, prm := range ev.Params {
		t := apply(prm.Type, parent.subst())
		data.add(prm.Name, t, SrcEvent, parent, prm.Node, prm)
		if v, ok := t.(*types.Value); ok {
			for _, f := range v.Decl.Fields {
				data.add(f.Name, f.Type, SrcEvent, parent, f.Node, f)
			}
		}
	}
	if n.IsBack() {
		node.Back = true
		return node
	}
	s := l.ui.find(n.Screen, n.Screen.Name)
	if s == nil {
		l.c.bag.Add(diag.E0703, rangeOf(n.Screen), diag.Message{Format: textUINoScreen, Args: []any{n.Screen.Name}})
		return nil
	}
	node.To = s
	node.ToArgs = l.screenArgs(x.with(data), s, n.Args, n.Screen)
	return node
}

// PartType is the type whose fields a part of the node binds: the type
// variable of the part with its inferred value.
func (n *Node) PartType(pt *Part) types.Type { return apply(pt.Type, n.subst()) }

// Infer infers the type variables of a primitive from the type of a field or an input.
func (p *Primitive) Infer(t types.Type) map[string]types.Type {
	s := subst{}
	unifyValue(p.BindType, t, s)
	return s.names()
}

// subst returns the inferred type variables of a node.
func (n *Node) subst() subst {
	if n.s == nil {
		return subst{}
	}
	return n.s
}

// ---------------------------------------------------------------------------
// Primitive parameters

// primArgs checks the parameters of a primitive: queries first, since they
// infer the row type variables; then values, fields and screens.
func (l *layout) primArgs(x *uiCtx, node *Node, args *ast.ArgList, s subst, at ast.Node, bound bool) {
	p := node.Prim
	given := map[string]*ast.Arg{}
	ok := bound
	if args != nil {
		for _, a := range args.Args {
			if a.Name == nil {
				l.c.bag.Add(diag.E0010, rangeOf(a.Value), quoteName(p.Name))
				ok = false
				continue
			}
			switch {
			case p.Param(a.Name.Name) == nil:
				d := l.c.bag.Add(diag.E0716, rangeOf(a.Name), diag.Message{Format: textUIPrimParam, Args: []any{p.Name, a.Name.Name}})
				var cands []string
				for _, pp := range p.Params {
					cands = append(cands, pp.Name)
				}
				if c := closest(a.Name.Name, cands); c != "" {
					d.Hint(hintDidYouMean, c)
				}
				ok = false
			case given[a.Name.Name] != nil:
				l.c.bag.Add(diag.E0205, rangeOf(a.Name), a.Name.Name)
			default:
				given[a.Name.Name] = a
			}
		}
	}
	vals := map[string]*PrimValue{}
	var order []*PrimParam
	for _, pp := range p.Params {
		if pp.Kind == ParamQuery || pp.Kind == ParamSearch {
			order = append(order, pp)
		}
	}
	for _, pp := range p.Params {
		if pp.Kind != ParamQuery && pp.Kind != ParamSearch {
			order = append(order, pp)
		}
	}
	for _, pp := range order {
		a := given[pp.Name]
		if a == nil {
			switch {
			case pp.Auto:
				if v := l.autoField(node, pp, s, at); v != nil {
					vals[pp.Name] = v
				} else {
					ok = false
				}
			case pp.Optional:
				vals[pp.Name] = &PrimValue{Param: pp}
			default:
				l.c.bag.Add(diag.E0716, rangeOf(at), diag.Message{Format: textUIPrimMissing, Args: []any{pp.Name, p.Name}})
				ok = false
			}
			continue
		}
		var v *PrimValue
		switch pp.Kind {
		case ParamValue:
			l.body(x).check(a.Value, pp.Type)
			v = &PrimValue{Param: pp, Expr: a.Value, Value: l.newExpr(x, a.Value, pp.Type)}
		case ParamQuery, ParamSearch:
			v = l.queryValue(x, pp, a.Value, s)
		case ParamField:
			v = l.fieldValue(pp, a.Value, s)
		case ParamScreen:
			v = l.screenValue(pp, a.Value)
		}
		if v == nil {
			ok = false
			continue
		}
		vals[pp.Name] = v
	}
	if ok {
		for _, tv := range p.Vars {
			if _, bound := s[tv]; !bound {
				l.c.bag.Add(diag.E0716, rangeOf(at), diag.Message{Format: textUIUnbound, Args: []any{tv.Name, p.Name}})
				break
			}
		}
	}
	node.Params = vals
	node.s = s
	node.Subst = s.names()
}

// queryValue is a query parameter: Type.Query(arguments); for a parameter
// with passed values (search(text string)) the query arguments are bound
// to them by name.
func (l *layout) queryValue(x *uiCtx, pp *PrimParam, e ast.Expr, s subst) *PrimValue {
	var sel *ast.SelectorExpr
	var args *ast.ArgList
	switch e := e.(type) {
	case *ast.CallExpr:
		sel, _ = e.Fun.(*ast.SelectorExpr)
		args = e.Args
	case *ast.SelectorExpr:
		sel = e
	}
	typ, _ := exprIdent(sel)
	if typ == nil {
		l.c.bag.Add(diag.E0716, rangeOf(e), diag.Message{Format: textUIPrimQuery, Args: []any{pp.Name}})
		return nil
	}
	owner, m := l.method(typ, sel.Sel)
	if m == nil {
		return nil
	}
	what := owner.Name + "." + m.Name
	if m.Kind != types.Query {
		l.c.bag.Add(diag.E0716, rangeOf(e), diag.Message{Format: textUIPrimQuery, Args: []any{pp.Name}})
		return nil
	}
	if !unify(pp.Type, m.Result, s) {
		l.c.bag.Add(diag.E0716, rangeOf(e), diag.Message{Format: textUIPrimResult, Args: []any{pp.Name, pp.Type.String(), what, m.Result.String()}})
		return nil
	}
	var provided *uiScope
	if pp.Kind == ParamSearch {
		provided = &uiScope{}
		for _, v := range pp.Provided {
			provided.add(v.Name, v.Type, SrcProvided, nil, v.Node, v)
		}
	}
	v := &PrimValue{Param: pp, Expr: e, Owner: owner, Method: m}
	v.Args = l.methodArgs(x, nil, m, args, provided, e)
	for _, a := range v.Args {
		if a.Source == SrcInput && !a.Default && !isOptional(a.Type) {
			hint := "…"
			if len(pp.Provided) > 0 {
				hint = pp.Provided[0].Name
			}
			l.c.bag.Add(diag.E0708, rangeOf(e), diag.Message{Format: textUISearchArg, Args: []any{a.Name, what, a.Name, hint}})
		}
	}
	return v
}

func exprIdent(sel *ast.SelectorExpr) (*ast.Ident, bool) {
	if sel == nil {
		return nil, false
	}
	id, ok := sel.X.(*ast.Ident)
	return id, ok
}

// fieldValue is a field<R[, K]> parameter: the name of a row field.
func (l *layout) fieldValue(pp *PrimParam, e ast.Expr, s subst) *PrimValue {
	rec, ok := apply(pp.Record, s).(*types.Value)
	id, isIdent := e.(*ast.Ident)
	if !ok {
		return nil // the row is not inferred: the error is already reported
	}
	if !isIdent {
		l.c.bag.Add(diag.E0716, rangeOf(e), diag.Message{Format: textUIPrimField, Args: []any{pp.Name, rec.Decl.Name}})
		return nil
	}
	f := rec.Decl.Field(id.Name)
	if f == nil {
		l.c.bag.Add(diag.E0716, rangeOf(e), diag.Message{Format: textUINoField, Args: []any{rec.Decl.Name, id.Name}})
		return nil
	}
	if pp.FieldType != nil {
		k := apply(pp.FieldType, s)
		if !unifyValue(k, f.Type, s) {
			l.c.bag.Add(diag.E0716, rangeOf(e), diag.Message{Format: textUIPrimFieldTyp, Args: []any{f.Name, f.Type.String(), pp.Name, k.String()}})
			return nil
		}
	}
	return &PrimValue{Param: pp, Expr: e, Field: f.Name}
}

// autoField is = auto: the only field of type K or the first string field.
func (l *layout) autoField(node *Node, pp *PrimParam, s subst, at ast.Node) *PrimValue {
	if pp.Kind != ParamField {
		return &PrimValue{Param: pp}
	}
	rec, ok := apply(pp.Record, s).(*types.Value)
	if !ok {
		return nil
	}
	if pp.FieldType == nil {
		for _, f := range rec.Decl.Fields {
			if isBasic(types.Unwrap(f.Type), types.String) {
				return &PrimValue{Param: pp, Field: f.Name}
			}
		}
		l.c.bag.Add(diag.E0716, rangeOf(at), diag.Message{Format: textUIAutoLabel, Args: []any{rec.Decl.Name, pp.Name}})
		return nil
	}
	k := apply(pp.FieldType, s)
	var found []string
	for _, f := range rec.Decl.Fields {
		if identical(types.Unwrap(f.Type), types.Unwrap(k)) || identicalUI(k, f.Type) {
			found = append(found, f.Name)
		}
	}
	switch len(found) {
	case 1:
		return &PrimValue{Param: pp, Field: found[0]}
	case 0:
		l.c.bag.Add(diag.E0716, rangeOf(at), diag.Message{Format: textUIAutoNone, Args: []any{rec.Decl.Name, k.String(), pp.Name}})
	default:
		l.c.bag.Add(diag.E0716, rangeOf(at), diag.Message{Format: textUIAutoMany, Args: []any{rec.Decl.Name, k.String(), pp.Name, joinTypes(found)}})
	}
	return nil
}

func (l *layout) screenValue(pp *PrimParam, e ast.Expr) *PrimValue {
	id, ok := e.(*ast.Ident)
	if !ok || l.ui.find(id, id.Name) == nil {
		l.c.bag.Add(diag.E0716, rangeOf(e), diag.Message{Format: textUIPrimScreen, Args: []any{pp.Name}})
		return nil
	}
	return &PrimValue{Param: pp, Expr: e, Screen: l.ui.find(id, id.Name)}
}

// ---------------------------------------------------------------------------
// Default primitives

func (l *layout) defaultFor(t types.Type, role Role) *Default {
	return l.ui.DefaultFor(l.apps, t, role)
}

// DefaultFor looks up the default primitive for a type: in apps apps, in the
// module, in the catalog; on one level the exact type wins over a constraint.
func (u *UI) DefaultFor(apps []*App, t types.Type, role Role) *Default {
	t = types.Unwrap(t)
	var levels [][]*Default
	for _, a := range apps {
		levels = append(levels, a.Defaults)
	}
	var module, std []*Default
	for _, d := range u.Defaults {
		if d.Std {
			std = append(std, d)
		} else {
			module = append(module, d)
		}
	}
	levels = append(levels, module, std)
	for _, level := range levels {
		var best *Default
		for _, d := range level {
			if d.Role != role || d.Prim == nil || !unify(d.Pattern, t, subst{}) {
				continue
			}
			if best == nil || countVars(d.Pattern) < countVars(best.Pattern) {
				best = d
			}
		}
		if best != nil {
			return best
		}
	}
	return nil
}
