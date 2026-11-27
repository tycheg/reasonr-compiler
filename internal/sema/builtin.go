package sema

import (
	"sort"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// Built-in functions, properties and methods (RFC §8.3).

var (
	textBuiltinArgs = "%s takes %s"
	textOneNumber   = "one number"
	textOneValue    = "one value"
	textTwoNumbers  = "two numbers of the same type"
	textRoundArgs   = "a number and a digit count: round(x, 2)"
	textArgsOf      = "arguments: %s"
)

type builtinFunc func(b *body, x *ast.CallExpr) types.Type

var builtinFuncs map[string]builtinFunc

func init() {
	builtinFuncs = map[string]builtinFunc{
		"int":      conversion(tInt),
		"bigint":   conversion(tBigint),
		"float":    conversion(tFloat),
		"decimal":  conversion(tDecimal),
		"string":   toString,
		"abs":      sameNumber,
		"floor":    sameNumber,
		"ceil":     sameNumber,
		"min":      twoNumbers,
		"max":      twoNumbers,
		"round":    round,
		"schedule": schedule,
	}
}

// positional returns the values of the positional arguments of a built-in
// function, or nil if there are not n of them or there are named ones (the
// error is reported).
func (b *body) positional(x *ast.CallExpr, n int, what string) []ast.Expr {
	ok := len(x.Args.Args) == n
	var out []ast.Expr
	for _, a := range x.Args.Args {
		if a.Name != nil {
			ok = false
		}
		out = append(out, a.Value)
	}
	if !ok {
		b.skipArgs(x.Args)
		b.c.bag.Add(diag.E0214, rangeOf(x), diag.Message{Format: textBuiltinArgs, Args: []any{b.c.quote(x.Fun), what}})
		return nil
	}
	return out
}

func conversion(to types.Type) builtinFunc {
	return func(b *body, x *ast.CallExpr) types.Type {
		args := b.positional(x, 1, textOneValue)
		if args == nil {
			return to
		}
		t := b.expr(args[0], nil)
		if types.IsInvalid(t) || b.optionalOperand(args[0], t) {
			return to
		}
		if !isNumeric(t) && !isBasic(t, types.String) {
			b.c.bag.Add(diag.E0220, rangeOf(args[0]), to.String()+"(…)", t.String())
		}
		return to
	}
}

func toString(b *body, x *ast.CallExpr) types.Type {
	if args := b.positional(x, 1, textOneValue); args != nil {
		b.expr(args[0], nil)
	}
	return tString
}

func sameNumber(b *body, x *ast.CallExpr) types.Type {
	args := b.positional(x, 1, textOneNumber)
	if args == nil {
		return invalid
	}
	t := b.expr(args[0], nil)
	return b.numberArg(args[0], t)
}

func (b *body) numberArg(x ast.Expr, t types.Type) types.Type {
	if types.IsInvalid(t) || b.optionalOperand(x, t) {
		return invalid
	}
	if !isNumeric(t) {
		b.c.bag.Add(diag.E0202, rangeOf(x), "a number", t.String())
		return invalid
	}
	return t
}

func twoNumbers(b *body, x *ast.CallExpr) types.Type {
	args := b.positional(x, 2, textTwoNumbers)
	if args == nil {
		return invalid
	}
	ta, tb := b.operands(args[0], args[1], nil)
	ta = b.numberArg(args[0], ta)
	if types.IsInvalid(ta) || types.IsInvalid(tb) {
		return invalid
	}
	if !identical(ta, tb) {
		b.mismatch(args[1], ta, tb)
	}
	return ta
}

func round(b *body, x *ast.CallExpr) types.Type {
	args := b.positional(x, 2, textRoundArgs)
	if args == nil {
		return invalid
	}
	t := b.numberArg(args[0], b.expr(args[0], nil))
	b.check(args[1], tInt)
	return t
}

// schedule(at: time, run: command(...)) is a built-in command returning Timer.Id.
func schedule(b *body, x *ast.CallExpr) types.Type {
	b.allow(x, actSchedule, ctxCommand, ctxOn)
	var at, run ast.Expr
	for _, a := range x.Args.Args {
		switch {
		case a.Name == nil:
			b.c.bag.Add(diag.E0010, rangeOf(a.Value), quoteName("schedule"))
			b.expr(a.Value, nil)
		case a.Name.Name == "at" && at == nil:
			at = a.Value
		case a.Name.Name == "run" && run == nil:
			run = a.Value
		default:
			b.c.bag.Add(diag.E0204, rangeOf(a.Name), quoteName("schedule"), a.Name.Name)
			b.expr(a.Value, nil)
		}
	}
	if at == nil || run == nil {
		b.c.bag.Add(diag.E0203, rangeOf(x), quoteName("schedule"), "at, run")
	}
	if at != nil {
		b.check(at, tTimestamp)
	}
	if run != nil {
		call, ok := run.(*ast.CallExpr)
		fun, isName := (*ast.Ident)(nil), false
		if ok {
			fun, isName = call.Fun.(*ast.Ident)
		}
		var m *types.Method
		if isName && b.owner != nil {
			m = b.owner.Method(fun.Name)
		}
		if m == nil || m.Kind != types.Command {
			if ok {
				b.call(call, nil, true)
			} else {
				b.expr(run, nil)
			}
			b.c.bag.Add(diag.E0402, rangeOf(run))
		} else {
			b.c.info.Calls[call] = &Call{Kind: CallMethod, Decl: b.owner, Method: m}
			b.args(call, quoteName(m.Name), m.Params)
			b.setType(call, tVoid)
		}
	}
	return &types.TimerID{}
}

// builtinProperty returns the type of a built-in property or nil.
func builtinProperty(recv types.Type, name string) types.Type {
	switch r := recv.(type) {
	case *types.Basic:
		switch {
		case r.Kind == types.String && name == "length":
			return tInt
		case r.Kind == types.Timestamp && name == "date":
			return tDate
		case r.Kind == types.Duration && name == "seconds":
			return tBigint
		}
	case *types.List, *types.Set:
		elem := elemOf(r)
		switch name {
		case "count":
			return tInt
		case "isEmpty":
			return tBool
		case "first", "last":
			return optional(elem)
		case "sum":
			if isNumeric(elem) {
				return elem
			}
		}
	case *types.Map:
		switch name {
		case "count":
			return tInt
		case "isEmpty":
			return tBool
		case "keys":
			return &types.List{Elem: r.Key}
		case "values":
			return &types.List{Elem: r.Elem}
		}
	case *types.Page:
		switch name {
		case "items":
			return &types.List{Elem: r.Elem}
		case "next":
			return optional(tString)
		}
	case *types.PageReq:
		switch name {
		case "size":
			return tInt
		case "cursor":
			return optional(tString)
		}
	case *types.Caller:
		if name == "roles" {
			return &types.Set{Elem: tString}
		}
	}
	return nil
}

// builtinSig is the signature of a built-in method.
type builtinSig struct {
	params []types.Type
	result types.Type
}

// builtinMethodSig returns the signature of a built-in method or nil.
func builtinMethodSig(recv types.Type, name string) *builtinSig {
	type sig = builtinSig
	var s *sig
	switch r := recv.(type) {
	case *types.Basic:
		switch r.Kind {
		case types.String:
			switch name {
			case "trim", "lower", "upper":
				s = &sig{nil, tString}
			case "startsWith", "contains", "matches":
				s = &sig{[]types.Type{tString}, tBool}
			}
		case types.Date, types.Timestamp:
			switch name {
			case "addDays":
				s = &sig{[]types.Type{tInt}, r}
			case "between":
				s = &sig{[]types.Type{r, r}, tBool}
			}
		}
	case *types.List, *types.Set:
		elem := elemOf(r)
		switch name {
		case "contains":
			s = &sig{[]types.Type{elem}, tBool}
		case "with", "without":
			s = &sig{[]types.Type{elem}, r}
		case "at":
			if _, isList := r.(*types.List); isList {
				s = &sig{[]types.Type{tInt}, optional(elem)}
			}
		}
	case *types.Map:
		switch name {
		case "get":
			s = &sig{[]types.Type{r.Key}, optional(r.Elem)}
		case "put":
			s = &sig{[]types.Type{r.Key, r.Elem}, r}
		case "contains":
			s = &sig{[]types.Type{r.Key}, tBool}
		}
	}
	return s
}

// builtinMemberNames are the names of all built-in properties and methods.
var builtinMemberNames = []string{
	"length", "date", "seconds", "count", "isEmpty", "first", "last", "sum", "keys", "values",
	"items", "next", "size", "cursor", "roles",
	"trim", "lower", "upper", "startsWith", "contains", "matches", "addDays", "between",
	"with", "without", "at", "get", "put",
}

// BuiltinMember is a built-in property or method of a value.
type BuiltinMember struct {
	Name   string
	Method bool
	Params []types.Type // for a method
	Type   types.Type   // the property type or the method result
}

// BuiltinMembers returns the built-in properties and methods of a value of
// type t (for completion) in name order.
func BuiltinMembers(t types.Type) []BuiltinMember {
	var out []BuiltinMember
	for _, name := range builtinMemberNames {
		if pt := builtinProperty(t, name); pt != nil {
			out = append(out, BuiltinMember{Name: name, Type: pt})
		} else if s := builtinMethodSig(t, name); s != nil {
			out = append(out, BuiltinMember{Name: name, Method: true, Params: s.params, Type: s.result})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// builtinMethod checks a built-in method; nil means the error is reported.
func (b *body) builtinMethod(x *ast.CallExpr, fun *ast.SelectorExpr, recv types.Type) types.Type {
	name := fun.Sel.Name
	s := builtinMethodSig(recv, name)
	if s == nil {
		b.skipArgs(x.Args)
		b.c.bag.Add(diag.E0221, rangeOf(fun.Sel), recv.String(), diag.Message{Format: textNoMethod, Args: []any{name}})
		return nil
	}
	if len(x.Args.Args) != len(s.params) {
		b.skipArgs(x.Args)
		names := make([]string, len(s.params))
		for i, p := range s.params {
			names[i] = p.String()
		}
		list := "—"
		if len(names) > 0 {
			list = joinTypes(names)
		}
		b.c.bag.Add(diag.E0214, rangeOf(x), diag.Message{Format: textBuiltinArgs, Args: []any{
			b.c.quote(x.Fun), diag.Message{Format: textArgsOf, Args: []any{list}}}})
		return s.result
	}
	for i, a := range x.Args.Args {
		if a.Name != nil {
			b.c.bag.Add(diag.E0204, rangeOf(a.Name), b.c.quote(x.Fun), a.Name.Name)
		}
		b.check(a.Value, s.params[i])
	}
	return s.result
}

func joinTypes(names []string) string {
	out := names[0]
	for _, n := range names[1:] {
		out += ", " + n
	}
	return out
}
