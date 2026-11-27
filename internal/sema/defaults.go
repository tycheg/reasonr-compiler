package sema

import (
	"math/big"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/constant"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// checkDefaults checks the default values of fields and parameters (RFC
// §7.3, plan §11.7): an explicit "= value" is a constant of the right type;
// an entity or projection field without one must have its type's default
// value or be T?.
func (c *checker) checkDefaults(td *types.Decl) {
	for _, f := range td.Fields {
		fd := f.Node.(*ast.FieldDecl)
		if fd.Default != nil {
			c.constDefault(fd.Default, f.Type)
		}
		if v, ok := c.fieldDefault(f); ok {
			c.info.Defaults[f] = v
		}
		if fd.Default != nil {
			continue
		}
		if !td.Kind.HasID() || isOptional(f.Type) || types.IsInvalid(f.Type) || c.hasDefault(f.Type) {
			continue
		}
		switch f.Type.(type) {
		case *types.ID, *types.TimerID:
			c.bag.Add(diag.E0210, rangeOf(fd.Type), f.Name, f.Type.String())
		default:
			c.bag.Add(diag.E0211, rangeOf(fd.Type), f.Type.String(), f.Name, f.Type.String())
		}
	}
	for _, m := range td.Methods {
		for _, p := range m.Params {
			if prm, ok := p.Node.(*ast.Param); ok && prm.Default != nil {
				c.constDefault(prm.Default, p.Type)
				if v, ok := constant.Eval(prm.Default, &constEnv{c: c}); ok {
					c.info.Defaults[p] = v
				}
			}
		}
	}
}

func (c *checker) constDefault(x ast.Expr, t types.Type) {
	b := c.newBody(ctxDefault, nil, nil)
	got := b.check(x, t)
	if types.IsInvalid(got) {
		return
	}
	if _, ok := constant.Eval(x, &constEnv{c: c}); !ok {
		c.bag.Add(diag.E0212, rangeOf(x))
	}
}

// hasDefault reports whether the type has a default value.
func (c *checker) hasDefault(t types.Type) bool {
	switch t := t.(type) {
	case *types.Basic, *types.Optional, *types.List, *types.Set, *types.Map, *types.PageReq, *types.Invalid:
		return true
	case *types.Enum:
		return len(t.Decl.Values) > 0
	case *types.Value:
		_, ok := c.valueDefault(t.Decl)
		return ok
	}
	return false
}

// States of computing the default value of a value object.
const (
	defUnknown = iota
	defYes
	defNo
	defBusy // being computed: the value object contains itself
)

// valueDefault computes the default value of a value object: all fields
// have default values, and the check invariants hold on them.
func (c *checker) valueDefault(d *types.Decl) (constant.Struct, bool) {
	if c.defaults == nil {
		c.defaults = map[*types.Decl]int{}
		c.defaultValues = map[*types.Decl]constant.Struct{}
	}
	switch c.defaults[d] {
	case defYes:
		return c.defaultValues[d], true
	case defNo, defBusy:
		return constant.Struct{}, false
	}
	c.defaults[d] = defBusy
	v, ok := c.computeDefault(d)
	if ok {
		c.defaults[d] = defYes
		c.defaultValues[d] = v
	} else {
		c.defaults[d] = defNo
	}
	return v, ok
}

func (c *checker) computeDefault(d *types.Decl) (constant.Struct, bool) {
	v := constant.Struct{Type: d.Name, Fields: map[string]constant.Value{}}
	for _, f := range d.Fields {
		fv, ok := c.fieldDefault(f)
		if !ok {
			return v, false
		}
		v.Fields[f.Name] = fv
	}
	n, ok := d.Node.(*ast.ValueDecl)
	if !ok {
		return v, true
	}
	env := &constEnv{c: c, fields: v.Fields}
	for _, m := range n.Members {
		chk, ok := m.(*ast.CheckDecl)
		if !ok {
			continue
		}
		r, ok := constant.Eval(chk.Cond, env)
		if b, isBool := r.(bool); !ok || !isBool || !b {
			return v, false
		}
	}
	return v, true
}

// fieldDefault is the default value of a field: explicit or the type's
// zero value. A value that refers to itself (A A = Positive() in Positive)
// is not computed.
func (c *checker) fieldDefault(f *types.Var) (constant.Value, bool) {
	if c.evaluating[f] {
		return nil, false
	}
	if c.evaluating == nil {
		c.evaluating = map[*types.Var]bool{}
	}
	c.evaluating[f] = true
	defer delete(c.evaluating, f)
	if fd, ok := f.Node.(*ast.FieldDecl); ok && fd.Default != nil {
		return constant.Eval(fd.Default, &constEnv{c: c})
	}
	return c.zero(f.Type)
}

// zero is the zero value of a type (RFC §7.1–7.3).
func (c *checker) zero(t types.Type) (constant.Value, bool) {
	switch t := t.(type) {
	case *types.Basic:
		switch t.Kind {
		case types.Int, types.Bigint, types.Float, types.Decimal:
			return new(big.Rat), true
		case types.String:
			return "", true
		case types.Bool:
			return false, true
		}
		return constant.Text("zero " + t.String()), true
	case *types.Enum:
		if len(t.Decl.Values) == 0 {
			return nil, false
		}
		return constant.Enum{Type: t.Decl.Name, Name: t.Decl.Values[0].Name}, true
	case *types.Optional:
		return constant.None{}, true
	case *types.List, *types.Set, *types.Map:
		return constant.List{}, true
	case *types.Value:
		return c.valueDefault(t.Decl)
	case *types.PageReq:
		return constant.Text("PageRequest"), true
	}
	return nil, false
}

// constEnv resolves names for package constant using the type check results.
type constEnv struct {
	c      *checker
	fields map[string]constant.Value // value object fields while computing check
}

func (e *constEnv) Ident(id *ast.Ident) (constant.Value, bool) {
	o := e.c.info.Uses[id]
	if o == nil {
		return nil, false
	}
	switch o.Kind {
	case ObjEnumValue:
		return constant.Enum{Type: o.Enum.Name, Name: o.Name}, true
	case ObjField:
		v, ok := e.fields[o.Name]
		return v, ok
	}
	return nil, false
}

func (e *constEnv) Selector(x *ast.SelectorExpr) (constant.Value, bool) {
	if o := e.c.info.Uses[x.Sel]; o != nil && o.Kind == ObjEnumValue {
		return constant.Enum{Type: o.Enum.Name, Name: o.Name}, true
	}
	return nil, false
}

func (e *constEnv) Construct(call *ast.CallExpr, args map[string]constant.Value) (constant.Value, bool) {
	k := e.c.info.Calls[call]
	if k == nil || k.Kind != CallConstruct {
		return nil, false
	}
	v := constant.Struct{Type: k.Decl.Name, Fields: map[string]constant.Value{}}
	for _, f := range k.Decl.Fields {
		if a, ok := args[f.Name]; ok {
			v.Fields[f.Name] = a
			continue
		}
		fv, ok := e.c.fieldDefault(f)
		if !ok {
			return nil, false
		}
		v.Fields[f.Name] = fv
	}
	return v, true
}
