// Package constant evaluates constant expressions: field defaults and the
// check invariants of value objects on those values
// (RFC §28). The caller resolves names through Env.
package constant

import (
	"math/big"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/token"
)

// Value is a constant: bool, *big.Rat (a number), string, Enum, None,
// Struct, List, and durations, dates and times in source form (Text).
type Value any

// Enum is an enum value.
type Enum struct{ Type, Name string }

// None is the absence of a value.
type None struct{}

// Struct is a value object: fields by name.
type Struct struct {
	Type   string
	Fields map[string]Value
}

// List is a list, set or map of constants (elements in order).
type List []Value

// Text is a literal that takes no part in evaluation: a duration, date or time.
type Text string

// Env resolves names: enum values, fields and value object constructors.
// ok == false means the name is not a constant.
type Env interface {
	// Ident returns the value of a name (an enum value, a field).
	Ident(id *ast.Ident) (Value, bool)
	// Selector returns the value of x.name for a type name (OrderStatus.searching).
	Selector(x *ast.SelectorExpr) (Value, bool)
	// Construct builds a value object from a call Type(...); args are the evaluated arguments.
	Construct(call *ast.CallExpr, args map[string]Value) (Value, bool)
}

// Eval evaluates an expression. ok == false means the expression is not a
// constant or cannot be evaluated (division by zero, unknown method).
func Eval(x ast.Expr, env Env) (Value, bool) {
	switch x := x.(type) {
	case *ast.BasicLit:
		return lit(x)
	case *ast.ParenExpr:
		return Eval(x.X, env)
	case *ast.Ident:
		return env.Ident(x)
	case *ast.SelectorExpr:
		if v, ok := env.Selector(x); ok {
			return v, true
		}
		base, ok := Eval(x.X, env)
		if !ok {
			return nil, false
		}
		if _, none := base.(None); none && x.Safe {
			return None{}, true
		}
		s, ok := base.(Struct)
		if !ok {
			return nil, false
		}
		v, ok := s.Fields[x.Sel.Name]
		return v, ok
	case *ast.UnaryExpr:
		v, ok := Eval(x.X, env)
		if !ok {
			return nil, false
		}
		switch x.Op {
		case token.SUB:
			if r, ok := v.(*big.Rat); ok {
				return new(big.Rat).Neg(r), true
			}
		case token.NOT:
			if b, ok := v.(bool); ok {
				return !b, true
			}
		}
		return nil, false
	case *ast.BinaryExpr:
		return binary(x, env)
	case *ast.IsNoneExpr:
		v, ok := Eval(x.X, env)
		if !ok {
			return nil, false
		}
		_, none := v.(None)
		return none != x.Not, true
	case *ast.BracketExpr:
		out := List{}
		for _, e := range x.Elems {
			v, ok := Eval(e, env)
			if !ok {
				return nil, false
			}
			out = append(out, v)
		}
		return out, true
	case *ast.MapLit:
		out := List{}
		for _, e := range x.Entries {
			k, ok1 := Eval(e.Key, env)
			v, ok2 := Eval(e.Value, env)
			if !ok1 || !ok2 {
				return nil, false
			}
			out = append(out, k, v)
		}
		return out, true
	case *ast.CallExpr:
		args := map[string]Value{}
		for _, a := range x.Args.Args {
			if a.Name == nil {
				return nil, false
			}
			v, ok := Eval(a.Value, env)
			if !ok {
				return nil, false
			}
			args[a.Name.Name] = v
		}
		return env.Construct(x, args)
	}
	return nil, false
}

func lit(x *ast.BasicLit) (Value, bool) {
	switch x.Kind {
	case token.INT, token.DECIMAL:
		r, ok := new(big.Rat).SetString(strings.ReplaceAll(x.Raw, "_", ""))
		return r, ok
	case token.STRING:
		return x.Value, true
	case token.TRUE:
		return true, true
	case token.FALSE:
		return false, true
	case token.NONE:
		return None{}, true
	case token.DURATION, token.DATE, token.TIMESTAMP:
		return Text(x.Raw), true
	}
	return nil, false
}

func binary(x *ast.BinaryExpr, env Env) (Value, bool) {
	a, ok := Eval(x.X, env)
	if !ok {
		return nil, false
	}
	// and, or and ?? are lazy.
	switch x.Op {
	case token.AND, token.OR:
		ab, ok := a.(bool)
		if !ok {
			return nil, false
		}
		if ab == (x.Op == token.OR) {
			return ab, true
		}
		b, ok := Eval(x.Y, env)
		bb, isBool := b.(bool)
		return bb, ok && isBool
	case token.COALESCE:
		if _, none := a.(None); !none {
			return a, true
		}
		return Eval(x.Y, env)
	}
	b, ok := Eval(x.Y, env)
	if !ok {
		return nil, false
	}
	switch x.Op {
	case token.EQ:
		return Equal(a, b), true
	case token.NE:
		return !Equal(a, b), true
	}
	if ar, ok := a.(*big.Rat); ok {
		br, ok := b.(*big.Rat)
		if !ok {
			return nil, false
		}
		return arith(x.Op, ar, br)
	}
	if as, ok := a.(string); ok {
		bs, ok := b.(string)
		if !ok {
			return nil, false
		}
		switch x.Op {
		case token.ADD:
			return as + bs, true
		case token.LT:
			return as < bs, true
		case token.LE:
			return as <= bs, true
		case token.GT:
			return as > bs, true
		case token.GE:
			return as >= bs, true
		}
	}
	return nil, false
}

func arith(op token.Kind, a, b *big.Rat) (Value, bool) {
	switch op {
	case token.ADD:
		return new(big.Rat).Add(a, b), true
	case token.SUB:
		return new(big.Rat).Sub(a, b), true
	case token.MUL:
		return new(big.Rat).Mul(a, b), true
	case token.QUO:
		if b.Sign() == 0 {
			return nil, false
		}
		return new(big.Rat).Quo(a, b), true
	case token.LT:
		return a.Cmp(b) < 0, true
	case token.LE:
		return a.Cmp(b) <= 0, true
	case token.GT:
		return a.Cmp(b) > 0, true
	case token.GE:
		return a.Cmp(b) >= 0, true
	}
	return nil, false
}

// Equal is structural equality of constants.
func Equal(a, b Value) bool {
	switch a := a.(type) {
	case *big.Rat:
		b, ok := b.(*big.Rat)
		return ok && a.Cmp(b) == 0
	case Struct:
		b, ok := b.(Struct)
		if !ok || a.Type != b.Type || len(a.Fields) != len(b.Fields) {
			return false
		}
		for k, v := range a.Fields {
			if !Equal(v, b.Fields[k]) {
				return false
			}
		}
		return true
	case List:
		b, ok := b.(List)
		if !ok || len(a) != len(b) {
			return false
		}
		for i := range a {
			if !Equal(a[i], b[i]) {
				return false
			}
		}
		return true
	}
	return a == b
}
