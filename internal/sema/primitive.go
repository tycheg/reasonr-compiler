package sema

import (
	"strings"
	"unicode"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/constant"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// UI primitives (RFC §22.7): declarations, type variables and their
// inference at the layout site, default primitives for types.

// BindKind is what a primitive is placed on in a layout.
type BindKind uint8

const (
	BindNone    BindKind = iota // a layout container
	BindQuery                   // a query with result BindType
	BindCommand                 // a command
	BindScreen                  // a screen
	BindField                   // a data field of type BindType: display
	BindInput                   // a parameter of type BindType: input
)

// PartBinds is what lowercase names mean in a part.
type PartBinds uint8

const (
	PartNone    PartBinds = iota // primitives only
	PartType                     // fields of a value of type Part.Type
	PartParams                   // query arguments not passed
	PartInputs                   // command inputs
	PartScreens                  // screens: navigation items
)

// ParamKind is the kind of a primitive parameter.
type ParamKind uint8

const (
	ParamValue  ParamKind = iota // a value of a simple type
	ParamQuery                   // a query with result Type
	ParamSearch                  // a query the primitive passes Provided to
	ParamField                   // the name of a field of row Record [of type FieldType]
	ParamScreen                  // a screen
)

// Primitive is a primitive declaration.
type Primitive struct {
	Name     string
	Decl     *types.Decl
	Node     *ast.PrimitiveDecl
	Std      bool // from the standard catalog
	Vars     []*types.TypeVar
	Params   []*PrimParam
	Binds    BindKind
	BindType types.Type // BindQuery, BindField, BindInput
	Children *Part      // what children written right under the primitive mean; nil: primitives only
	Parts    []*Part
	Events   []*PrimEvent
}

// Part returns a part by name or nil.
func (p *Primitive) Part(name string) *Part {
	for _, pt := range p.Parts {
		if pt.Name == name {
			return pt
		}
	}
	return nil
}

// Param returns a parameter by name or nil.
func (p *Primitive) Param(name string) *PrimParam {
	for _, pp := range p.Params {
		if pp.Name == name {
			return pp
		}
	}
	return nil
}

// Event returns an event by name or nil.
func (p *Primitive) Event(name string) *PrimEvent {
	for _, e := range p.Events {
		if e.Name == name {
			return e
		}
	}
	return nil
}

// Part is a part of a primitive or its children.
type Part struct {
	Name  string // "" for children
	Node  *ast.PartItem
	Binds PartBinds
	Type  types.Type // PartType
}

// PrimParam is a primitive parameter.
type PrimParam struct {
	Name      string
	Node      *ast.PrimParam
	Kind      ParamKind
	Type      types.Type   // ParamValue, ParamQuery, ParamSearch: the result type
	Provided  []*types.Var // ParamSearch
	Record    types.Type   // ParamField: the row type
	FieldType types.Type   // ParamField: the field type or nil
	Optional  bool         // has a default value or = auto
	Auto      bool
	// Default is the default value of a ParamValue if it is a constant.
	Default constant.Value
}

// PrimEvent is a primitive event.
type PrimEvent struct {
	Name   string
	Node   *ast.PrimEvent
	Params []*types.Var
}

// PrimValue is the value of a primitive parameter in a layout or default.
type PrimValue struct {
	Param  *PrimParam
	Expr   ast.Expr      // as written; nil for the default value or auto
	Value  *Expr         // ParamValue: the value
	Owner  *types.Decl   // the query: ParamQuery, ParamSearch
	Method *types.Method // the query
	Args   []*Arg        // the query arguments
	Field  string        // ParamField
	Screen *Screen       // ParamScreen
}

// Default is the default primitive for a type.
type Default struct {
	Node    *ast.DefaultDecl
	Pattern types.Type // a type or a pattern with the constraints id, enum, value
	Prim    *Primitive
	Role    Role
	App     *App // nil for the module or the catalog
	Std     bool
	Params  map[string]*PrimValue
	Subst   map[string]types.Type
}

var (
	textUIVarShadow  = "type variable %q is repeated"
	textUIVarUnused  = "type variable %q cannot be inferred: it appears neither in binds nor in parameter types"
	textUIBindsTwice = "binds is given twice"
	textUIChildren2  = "children is given twice"
	textUIBindsType  = "binds without a kind is a query result: page<T>, list<T>, a value or a type variable, not %s"
	textUIPartType   = "a part binds the fields of a value: it needs a value type or a type variable, not %s"
	textUIPartNeeds  = "%s in a part needs a primitive that binds %s"
	textUIParamType  = "parameter %q: type %s does not fit — a simple type, a query (list<T>, page<T>, a value), field<T> or screen"
	textUIAutoField  = "= auto is only allowed for field<…> parameters"
	textUIDefault    = "the default of parameter %q must be a constant of its type"
	textUIDupName    = "%q is repeated in primitive %s"
	textUIUnion      = "type alternatives (|) are only allowed in binds and query parameters"
	textUIReserved   = "name %q is taken by a primitive implementation property: %s"
)

// runtimeProps are the properties of primitive implementations the runtime
// sets (compiler/uigen, --ui-types): parameters and parts cannot have these names.
var runtimeProps = map[string]bool{
	"data": true, "loading": true, "loadMore": true, "hasMore": true, "submit": true, "busy": true,
	"errors": true, "value": true, "onChange": true, "error": true, "href": true, "go": true,
	"caption": true, "rr": true, "children": true,
}

func reservedProp(name string) bool {
	return runtimeProps[name] || len(name) > 2 && strings.HasPrefix(name, "on") && unicode.IsUpper([]rune(name)[2])
}

var (
	textUIPropList = "data, loading, loadMore, hasMore, submit, busy, errors, value, onChange, error, href, go, caption, rr, children, on…"
)

// primitiveDecl checks a primitive declaration and builds a Primitive.
func (c *checker) primitiveDecl(d *types.Decl, n *ast.PrimitiveDecl, std bool) *Primitive {
	p := &Primitive{Name: d.Name, Decl: d, Node: n, Std: std}
	vars := map[string]*types.TypeVar{}
	if n.TypeParams != nil {
		for _, tp := range n.TypeParams.List {
			c.upperName(tp.Name, kindTypeVar)
			if vars[tp.Name.Name] != nil {
				c.bag.Add(diag.E0701, rangeOf(tp.Name), diag.Message{Format: textUIVarShadow, Args: []any{tp.Name.Name}})
				continue
			}
			v := &types.TypeVar{Name: tp.Name.Name}
			if tp.Constraint != nil {
				v.Constraint = tp.Constraint.Name
			}
			vars[v.Name] = v
			p.Vars = append(p.Vars, v)
		}
	}
	used := map[*types.TypeVar]bool{}
	names := map[string]bool{}
	dup := func(id *ast.Ident) bool {
		if names[id.Name] {
			c.bag.Add(diag.E0701, rangeOf(id), diag.Message{Format: textUIDupName, Args: []any{id.Name, p.Name}})
			return true
		}
		names[id.Name] = true
		return false
	}
	if n.Params != nil {
		for _, pp := range n.Params.List {
			c.lowerName(pp.Name, kindParam)
			if dup(pp.Name) {
				continue
			}
			if reservedProp(pp.Name.Name) {
				c.bag.Add(diag.E0701, rangeOf(pp.Name), diag.Message{Format: textUIReserved, Args: []any{pp.Name.Name, textUIPropList}})
				continue
			}
			if prm := c.primParam(p, pp, vars, used); prm != nil {
				p.Params = append(p.Params, prm)
			}
		}
	}
	var binds *ast.BindsItem
	for _, it := range n.Items {
		switch it := it.(type) {
		case *ast.BindsItem:
			if binds != nil {
				c.bag.Add(diag.E0701, rangeOf(it), textUIBindsTwice)
				continue
			}
			binds = it
			c.primBinds(p, it, vars, used)
		case *ast.PartItem:
			if it.Children {
				if p.Children != nil {
					c.bag.Add(diag.E0701, rangeOf(it), textUIChildren2)
					continue
				}
			} else {
				c.upperName(it.Name, kindPart)
				if dup(it.Name) {
					continue
				}
			}
			pt := c.primPart(it, vars)
			if it.Children {
				p.Children = pt
			} else {
				p.Parts = append(p.Parts, pt)
			}
		case *ast.PrimEvent:
			c.lowerName(it.Name, kindEvent)
			if dup(it.Name) {
				continue
			}
			ev := &PrimEvent{Name: it.Name.Name, Node: it}
			if it.Params != nil {
				for _, prm := range it.Params.List {
					t := c.uiType(prm.Type, vars, nil, false)
					ev.Params = append(ev.Params, &types.Var{Name: prm.Name.Name, Node: prm, Type: t})
				}
			}
			p.Events = append(p.Events, ev)
		}
	}
	// Parts with params and inputs need a suitable binding.
	for _, pt := range append([]*Part{p.Children}, p.Parts...) {
		if pt == nil {
			continue
		}
		switch {
		case pt.Binds == PartParams && p.Binds != BindQuery:
			c.bag.Add(diag.E0701, rangeOf(pt.Node), diag.Message{Format: textUIPartNeeds, Args: []any{"params", "binds <query result>"}})
		case pt.Binds == PartInputs && p.Binds != BindCommand:
			c.bag.Add(diag.E0701, rangeOf(pt.Node), diag.Message{Format: textUIPartNeeds, Args: []any{"inputs", "binds command"}})
		}
	}
	for _, v := range p.Vars {
		if !used[v] {
			for _, tp := range n.TypeParams.List {
				if tp.Name.Name == v.Name {
					c.bag.Add(diag.E0701, rangeOf(tp.Name), diag.Message{Format: textUIVarUnused, Args: []any{v.Name}})
				}
			}
		}
	}
	return p
}

func (c *checker) primBinds(p *Primitive, it *ast.BindsItem, vars map[string]*types.TypeVar, used map[*types.TypeVar]bool) {
	switch it.Kind {
	case ast.BindCommand:
		p.Binds = BindCommand
	case ast.BindScreen:
		p.Binds = BindScreen
	case ast.BindField, ast.BindInput:
		p.Binds = BindField
		if it.Kind == ast.BindInput {
			p.Binds = BindInput
		}
		p.BindType = c.uiType(it.Type, vars, used, true)
	default:
		p.Binds = BindQuery
		t := c.uiType(it.Type, vars, used, true)
		p.BindType = t
		for _, alt := range alternatives(t) {
			if !queryResultShape(alt) {
				c.bag.Add(diag.E0701, rangeOf(it.Type), diag.Message{Format: textUIBindsType, Args: []any{alt.String()}})
				break
			}
		}
	}
}

// queryResultShape: page<T>, list<T>, a value object or a type variable.
func queryResultShape(t types.Type) bool {
	switch t := t.(type) {
	case *types.Page:
		return true
	case *types.List:
		return true
	case *types.Value, *types.Invalid:
		return true
	case *types.TypeVar:
		return t.Constraint == "" || t.Constraint == "value"
	}
	return false
}

func (c *checker) primPart(it *ast.PartItem, vars map[string]*types.TypeVar) *Part {
	pt := &Part{Node: it}
	if it.Name != nil {
		pt.Name = it.Name.Name
	}
	switch it.Kind {
	case ast.PartParams:
		pt.Binds = PartParams
	case ast.PartInputs:
		pt.Binds = PartInputs
	case ast.PartScreens:
		pt.Binds = PartScreens
	default:
		if it.Type == nil {
			return pt
		}
		pt.Binds = PartType
		pt.Type = c.uiType(it.Type, vars, nil, false)
		switch t := pt.Type.(type) {
		case *types.Value, *types.Invalid:
		case *types.TypeVar:
			if t.Constraint != "" && t.Constraint != "value" {
				c.bag.Add(diag.E0701, rangeOf(it.Type), diag.Message{Format: textUIPartType, Args: []any{t.String()}})
			}
		default:
			c.bag.Add(diag.E0701, rangeOf(it.Type), diag.Message{Format: textUIPartType, Args: []any{t.String()}})
		}
	}
	return pt
}

func (c *checker) primParam(p *Primitive, pp *ast.PrimParam, vars map[string]*types.TypeVar, used map[*types.TypeVar]bool) *PrimParam {
	prm := &PrimParam{Name: pp.Name.Name, Node: pp, Optional: pp.Default != nil || pp.IsAuto(), Auto: pp.IsAuto()}
	if g, ok := pp.Type.(*ast.GenericType); ok && g.Name.Name == "field" && pp.Query == nil {
		prm.Kind = ParamField
		if len(g.Args) < 1 || len(g.Args) > 2 {
			c.bag.Add(diag.E0109, rangeOf(g), "field takes one or two type arguments: field<R> or field<R, K>")
			return nil
		}
		prm.Record = c.uiType(g.Args[0], vars, used, false)
		if len(g.Args) == 2 {
			prm.FieldType = c.uiType(g.Args[1], vars, used, false)
		}
		return prm
	}
	if pp.IsAuto() {
		c.bag.Add(diag.E0701, rangeOf(pp), textUIAutoField)
	}
	if nt, ok := pp.Type.(*ast.NamedType); ok && nt.String() == "screen" && pp.Query == nil {
		prm.Kind = ParamScreen
		return prm
	}
	t := c.uiType(pp.Type, vars, used, true)
	prm.Type = t
	if pp.Query != nil {
		prm.Kind = ParamSearch
		prm.Provided = c.params(pp.Query)
	} else if !hasVars(t) && simpleParam(t) {
		prm.Kind = ParamValue
	} else {
		prm.Kind = ParamQuery
	}
	ok := true
	for _, alt := range alternatives(t) {
		if prm.Kind != ParamValue && !queryResultShape(alt) {
			ok = false
		}
	}
	if !ok {
		c.bag.Add(diag.E0701, rangeOf(pp.Type), diag.Message{Format: textUIParamType, Args: []any{prm.Name, t.String()}})
		return nil
	}
	if pp.Default != nil {
		if prm.Kind != ParamValue {
			c.bag.Add(diag.E0701, rangeOf(pp.Default), diag.Message{Format: textUIDefault, Args: []any{prm.Name}})
		} else {
			b := c.newBody(ctxDefault, nil, nil)
			b.check(pp.Default, t)
			prm.Default, _ = constant.Eval(pp.Default, &constEnv{c: c})
		}
	}
	return prm
}

// simpleParam is the type of a value parameter: a simple type or an enum.
func simpleParam(t types.Type) bool {
	switch t := types.Unwrap(t).(type) {
	case *types.Basic, *types.Enum, *types.Invalid:
		return true
	case *types.List:
		return simpleParam(t.Elem)
	case *types.Set:
		return simpleParam(t.Elem)
	}
	return false
}

// uiType resolves a type in a primitive declaration or default: type
// variables vars, the constraints id, enum, value (in default, vars ==
// nil), variants with | (if union).
func (c *checker) uiType(t ast.TypeExpr, vars map[string]*types.TypeVar, used map[*types.TypeVar]bool, union bool) types.Type {
	switch t := t.(type) {
	case *ast.UnionType:
		if !union {
			c.bag.Add(diag.E0701, rangeOf(t), textUIUnion)
		}
		u := &types.Union{}
		for _, a := range t.Types {
			u.Alts = append(u.Alts, c.uiType(a, vars, used, false))
		}
		return u
	case *ast.NamedType:
		if len(t.Path) == 1 {
			name := t.Path[0].Name
			if v := vars[name]; v != nil {
				if used != nil {
					used[v] = true
				}
				return v
			}
			if vars == nil {
				switch name {
				case "id", "enum", "value":
					return &types.TypeVar{Name: name, Constraint: name}
				}
			}
		}
	case *ast.GenericType:
		if _, ok := generics[t.Name.Name]; ok && len(t.Args) == generics[t.Name.Name] {
			args := make([]types.Type, len(t.Args))
			for i, a := range t.Args {
				args[i] = c.uiType(a, vars, used, false)
			}
			switch t.Name.Name {
			case "list":
				return &types.List{Elem: args[0]}
			case "set":
				return &types.Set{Elem: args[0]}
			case "page":
				return &types.Page{Elem: args[0]}
			}
			return &types.Map{Key: args[0], Elem: args[1]}
		}
	case *ast.OptionalType:
		elem := c.uiType(t.Elem, vars, used, false)
		if types.IsInvalid(elem) {
			return invalid
		}
		return &types.Optional{Elem: elem}
	}
	return c.resolveType(t)
}

func alternatives(t types.Type) []types.Type {
	if u, ok := t.(*types.Union); ok {
		return u.Alts
	}
	return []types.Type{t}
}

func hasVars(t types.Type) bool {
	switch t := t.(type) {
	case *types.TypeVar:
		return true
	case *types.Union:
		for _, a := range t.Alts {
			if hasVars(a) {
				return true
			}
		}
	case *types.Optional:
		return hasVars(t.Elem)
	case *types.List:
		return hasVars(t.Elem)
	case *types.Set:
		return hasVars(t.Elem)
	case *types.Page:
		return hasVars(t.Elem)
	case *types.Map:
		return hasVars(t.Key) || hasVars(t.Elem)
	}
	return false
}

// countVars is the number of type variables in a pattern: the fewer, the more precise the default.
func countVars(t types.Type) int {
	switch t := t.(type) {
	case *types.TypeVar:
		return 1
	case *types.Optional:
		return countVars(t.Elem)
	case *types.List:
		return countVars(t.Elem)
	case *types.Set:
		return countVars(t.Elem)
	case *types.Page:
		return countVars(t.Elem)
	case *types.Map:
		return countVars(t.Key) + countVars(t.Elem)
	}
	return 0
}

// ---------------------------------------------------------------------------
// Type variable inference

// subst holds the values of type variables.
type subst map[*types.TypeVar]types.Type

func (s subst) copy() subst {
	out := make(subst, len(s))
	for k, v := range s {
		out[k] = v
	}
	return out
}

// satisfies reports whether a type meets the constraint of a type variable.
// A default constraint variable (rigid) meets the same constraint.
func satisfies(t types.Type, constraint string) bool {
	if rv, ok := t.(*types.TypeVar); ok {
		return constraint == "" || rv.Constraint == constraint
	}
	switch constraint {
	case "id":
		_, ok := t.(*types.ID)
		return ok
	case "enum":
		_, ok := t.(*types.Enum)
		return ok
	case "value":
		_, ok := t.(*types.Value)
		return ok
	}
	return true
}

// unify matches pattern p against type t and extends s. Variables in t
// (default constraints) are matched as concrete types.
func unify(p, t types.Type, s subst) bool {
	if types.IsInvalid(t) || types.IsInvalid(p) {
		return true
	}
	switch p := p.(type) {
	case *types.TypeVar:
		if bound, ok := s[p]; ok {
			return identicalUI(bound, t)
		}
		if !satisfies(t, p.Constraint) {
			return false
		}
		s[p] = t
		return true
	case *types.Union:
		for _, a := range p.Alts {
			try := s.copy()
			if unify(a, t, try) {
				for k, v := range try {
					s[k] = v
				}
				return true
			}
		}
		return false
	case *types.Optional:
		o, ok := t.(*types.Optional)
		return ok && unify(p.Elem, o.Elem, s)
	case *types.List:
		l, ok := t.(*types.List)
		return ok && unify(p.Elem, l.Elem, s)
	case *types.Set:
		l, ok := t.(*types.Set)
		return ok && unify(p.Elem, l.Elem, s)
	case *types.Page:
		l, ok := t.(*types.Page)
		return ok && unify(p.Elem, l.Elem, s)
	case *types.Map:
		m, ok := t.(*types.Map)
		return ok && unify(p.Key, m.Key, s) && unify(p.Elem, m.Elem, s)
	}
	return identicalUI(p, t)
}

// unifyValue is like unify, but a value T? fits a pattern for T: a field
// string? shows Text<T> with T = string?, and DateTime (timestamp) shows a
// timestamp? field.
func unifyValue(p, t types.Type, s subst) bool {
	try := s.copy()
	if unify(p, t, try) {
		for k, v := range try {
			s[k] = v
		}
		return true
	}
	if o, ok := t.(*types.Optional); ok {
		return unify(p, o.Elem, s)
	}
	return false
}

func identicalUI(a, b types.Type) bool {
	if av, ok := a.(*types.TypeVar); ok {
		bv, ok := b.(*types.TypeVar)
		return ok && av.Constraint == bv.Constraint
	}
	return identical(a, b)
}

// apply substitutes the values of type variables; uninferred ones remain.
func apply(t types.Type, s subst) types.Type {
	switch t := t.(type) {
	case *types.TypeVar:
		if v, ok := s[t]; ok {
			return v
		}
	case *types.Optional:
		return &types.Optional{Elem: apply(t.Elem, s)}
	case *types.List:
		return &types.List{Elem: apply(t.Elem, s)}
	case *types.Set:
		return &types.Set{Elem: apply(t.Elem, s)}
	case *types.Page:
		return &types.Page{Elem: apply(t.Elem, s)}
	case *types.Map:
		return &types.Map{Key: apply(t.Key, s), Elem: apply(t.Elem, s)}
	case *types.Union:
		u := &types.Union{}
		for _, a := range t.Alts {
			u.Alts = append(u.Alts, apply(a, s))
		}
		return u
	}
	return t
}

// names returns the values of variables by name, for Node.Subst.
func (s subst) names() map[string]types.Type {
	out := make(map[string]types.Type, len(s))
	for k, v := range s {
		out[k.Name] = v
	}
	return out
}
