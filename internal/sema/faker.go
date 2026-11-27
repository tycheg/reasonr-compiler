package sema

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// faker and range are tools of tests and demo scenarios (RFC §27.5,
// decision 3.3): plausible values, deterministic by the run seed.

// fakerFunc is a faker function: its parameters and result; nil in the
// parameters is a list of any type (pick), and its element is the result.
type fakerFunc struct {
	params []types.Type
	result types.Type
}

var fakerFuncs = map[string]fakerFunc{
	"firstName": {nil, tString}, "lastName": {nil, tString}, "name": {nil, tString},
	"phone": {nil, tString}, "email": {nil, tString}, "address": {nil, tString}, "company": {nil, tString},
	"text":    {[]types.Type{tInt}, tString},
	"int":     {[]types.Type{tInt, tInt}, tInt},
	"decimal": {[]types.Type{tDecimal, tDecimal}, tDecimal},
	"float":   {[]types.Type{tFloat, tFloat}, tFloat},
	"chance":  {[]types.Type{tDecimal}, tBool},
	"date":    {[]types.Type{tDate, tDate}, tDate},
	"pattern": {[]types.Type{tString}, tString},
	"pick":    {[]types.Type{nil}, nil},
}

// isFaker reports whether this is a call faker.function(…): the name faker is not taken.
func (b *body) isFaker(fun *ast.SelectorExpr) bool {
	id, ok := fun.X.(*ast.Ident)
	return ok && id.Name == "faker" && b.lookupQuiet("faker") == nil && !b.fieldNamed("faker")
}

// fakerCall checks faker.function(arguments).
func (b *body) fakerCall(x *ast.CallExpr, fun *ast.SelectorExpr) types.Type {
	if b.kind != ctxTest {
		b.c.bag.Add(diag.E0222, rangeOf(x.Fun), "faker")
		b.skipArgs(x.Args)
		return invalid
	}
	f, ok := fakerFuncs[fun.Sel.Name]
	if !ok {
		b.c.bag.Add(diag.E0221, rangeOf(fun.Sel), "faker", b.c.quote(fun.Sel))
		b.skipArgs(x.Args)
		return invalid
	}
	args := b.positional(x, len(f.params), "positional arguments")
	if args == nil && len(f.params) > 0 { // the error is reported
		return f.result
	}
	b.c.info.Calls[x] = &Call{Kind: CallBuiltin, Builtin: "faker." + fun.Sel.Name}
	if fun.Sel.Name == "pick" {
		t := b.expr(args[0], nil)
		if l, ok := types.Unwrap(t).(*types.List); ok {
			return l.Elem
		}
		if !types.IsInvalid(t) {
			b.c.bag.Add(diag.E0214, rangeOf(args[0]), diag.Message{Format: textBuiltinArgs, Args: []any{"faker.pick", "a list"}})
		}
		return invalid
	}
	for i, a := range args {
		b.check(a, f.params[i])
	}
	return f.result
}

// rangeCall checks range(n): a list of numbers from 0 to n−1.
func (b *body) rangeCall(x *ast.CallExpr) types.Type {
	if b.kind != ctxTest {
		b.c.bag.Add(diag.E0222, rangeOf(x.Fun), "range")
		b.skipArgs(x.Args)
		return invalid
	}
	args := b.positional(x, 1, "one number")
	if args == nil {
		return &types.List{Elem: tInt}
	}
	b.check(args[0], tInt)
	b.c.info.Calls[x] = &Call{Kind: CallBuiltin, Builtin: "range"}
	return &types.List{Elem: tInt}
}
