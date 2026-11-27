package lower

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/ir"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/token"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

func (f *fn) typeOf(x ast.Expr) types.Type {
	t, ok := f.l.info.Types[x]
	if !ok {
		panic(fmt.Sprintf("lower: no expression type %T", x))
	}
	return t
}

// expr lowers an expression and returns an operand with its value.
func (f *fn) expr(x ast.Expr) ir.Operand {
	switch x := x.(type) {
	case *ast.Ident:
		return f.ident(x)
	case *ast.BasicLit:
		return f.konst(literal(x, f.typeOf(x)))
	case *ast.InterpString:
		return f.interp(x)
	case *ast.ParenExpr:
		return f.expr(x.X)
	case *ast.UnaryExpr:
		return f.unary(x)
	case *ast.BinaryExpr:
		return f.binary(x)
	case *ast.IsNoneExpr:
		v := f.expr(x.X)
		if c, ok := f.constOf(v); ok {
			return f.konst(boolConst(c.None != x.Not))
		}
		r := f.value(ir.Basic(ir.TBool), ir.Instr{Op: ir.OpIsNone, Args: []ir.Operand{v}})
		if x.Not {
			r = f.value(ir.Basic(ir.TBool), ir.Instr{Op: ir.OpNot, Args: []ir.Operand{r}})
		}
		return r
	case *ast.InExpr:
		return f.in(x)
	case *ast.SelectorExpr:
		if r, ok := f.perfStat(x); ok {
			return r
		}
		return f.selector(x)
	case *ast.CallExpr:
		return f.call(x)
	case *ast.BracketExpr:
		return f.bracket(x)
	case *ast.MapLit:
		var args []ir.Operand
		for _, e := range x.Entries {
			args = append(args, f.expr(e.Key), f.expr(e.Value))
		}
		return f.value(irType(f.typeOf(x)), ir.Instr{Op: ir.OpCollMap, Args: args})
	case *ast.SearchExpr:
		return f.search(x)
	case *ast.NewIDExpr:
		id := f.typeOf(x).(*types.ID)
		return f.value(irType(id), ir.Instr{Op: ir.OpNewID, Sym: id.Owner.Name})
	case *ast.AllExpr:
		return f.value(irType(f.typeOf(x)), ir.Instr{Op: ir.OpAll, Sym: f.l.info.TypeRefs[x].Name})
	}
	panic(fmt.Sprintf("lower: expression %T is not lowered", x))
}

// narrow is the value v of expression x with the type sema narrowed: inside
// if x is not none, an optional value is copied into a non-optional slot.
// At run time the value is the same; only the slot type, which the bytecode
// verifier checks, changes.
func (f *fn) narrow(v ir.Operand, x ast.Expr) ir.Operand {
	st, ok := f.l.info.Types[x]
	if !ok || v.IsConst() {
		return v // a name without an expression type: a test step target, as
	}
	t := irType(st)
	if f.typeOfOperand(v).Equal(t) {
		return v
	}
	return f.value(t, ir.Instr{Op: ir.OpMove, Args: []ir.Operand{v}})
}

func (f *fn) ident(id *ast.Ident) ir.Operand {
	o := f.l.info.Uses[id]
	if o == nil {
		panic("lower: the name is not resolved: " + id.Name)
	}
	switch o.Kind {
	case sema.ObjLocal, sema.ObjParam, sema.ObjFixture:
		s, ok := f.slots[o.Node]
		if !ok {
			panic("lower: no slot for " + id.Name)
		}
		return f.narrow(ir.SlotOp(s), id)
	case sema.ObjField:
		// The type with narrowing: inside if x is not none the field is not optional.
		t := irType(f.typeOf(id))
		if n := len(f.rows); n > 0 {
			return f.value(t, ir.Instr{Op: ir.OpRecGet, Sym: id.Name, Args: []ir.Operand{f.rows[n-1]}})
		}
		return f.value(t, ir.Instr{Op: ir.OpField, Sym: id.Name})
	case sema.ObjEnumValue:
		return f.konst(ir.Const{Type: &ir.Type{Kind: ir.TEnum, Name: o.Enum.Name}, Text: o.Name})
	case sema.ObjFeature:
		return f.value(ir.Basic(ir.TBool), ir.Instr{Op: ir.OpFeature, Sym: o.Name})
	case sema.ObjBuiltin:
		if s, ok := f.builtins[o.Name]; ok {
			return ir.SlotOp(s)
		}
		switch o.Name {
		case "hasErrors":
			return f.value(ir.Basic(ir.TBool), ir.Instr{Op: ir.OpHasErrors})
		case "after":
			return f.value(irType(o.Type), ir.Instr{Op: ir.OpAfter})
		}
	}
	panic("lower: the name is not lowered: " + id.Name)
}

// literal returns the constant of a literal with its type from type checking.
func literal(x *ast.BasicLit, t types.Type) ir.Const {
	if x.Kind == token.NONE {
		return ir.Const{Type: irType(t), None: true}
	}
	it := irType(types.Unwrap(t))
	switch x.Kind {
	case token.INT, token.DECIMAL:
		raw := strings.ReplaceAll(x.Raw, "_", "")
		if it.Kind == ir.TFloat {
			v, _ := strconv.ParseFloat(raw, 64)
			return ir.Const{Type: it, Text: strconv.FormatFloat(v, 'g', -1, 64)}
		}
		r, _ := new(big.Rat).SetString(raw)
		text, _ := ir.RatText(r)
		return ir.Const{Type: it, Text: text}
	case token.DURATION:
		ns, _ := ir.ParseDuration(x.Raw)
		return ir.Const{Type: it, Text: strconv.FormatInt(ns, 10)}
	case token.STRING, token.DATE, token.TIMESTAMP:
		return ir.Const{Type: it, Text: x.Value}
	case token.TRUE:
		return boolConst(true)
	case token.FALSE:
		return boolConst(false)
	}
	panic("lower: the literal is not lowered: " + x.Raw)
}

func boolConst(b bool) ir.Const {
	return ir.Const{Type: ir.Basic(ir.TBool), Text: strconv.FormatBool(b)}
}

func intConst(n int64) ir.Const {
	return ir.Const{Type: ir.Basic(ir.TInt), Text: strconv.FormatInt(n, 10)}
}

func strConst(s string) ir.Const { return ir.Const{Type: ir.Basic(ir.TString), Text: s} }

// interp is a string with interpolation: the parts are concatenated,
// non-strings are converted with the string function.
func (f *fn) interp(x *ast.InterpString) ir.Operand {
	var parts []ir.Operand
	for _, p := range x.Parts {
		if p.X == nil {
			if p.Text != "" {
				parts = append(parts, f.konst(strConst(p.Text)))
			}
			continue
		}
		v := f.expr(p.X)
		if t := f.typeOf(p.X); !isBasic(t, types.String) {
			v = f.value(ir.Basic(ir.TString), ir.Instr{Op: ir.OpIntrinsic, Sym: "string", Args: []ir.Operand{v}})
		}
		parts = append(parts, v)
	}
	switch len(parts) {
	case 0:
		return f.konst(strConst(""))
	case 1:
		if f.typeOfOperand(parts[0]).Kind == ir.TString {
			return parts[0]
		}
	}
	return f.value(ir.Basic(ir.TString), ir.Instr{Op: ir.OpIntrinsic, Sym: "str.concat", Args: parts})
}

func isBasic(t types.Type, k types.BasicKind) bool {
	b, ok := t.(*types.Basic)
	return ok && b.Kind == k
}

func (f *fn) unary(x *ast.UnaryExpr) ir.Operand {
	if x.Op == token.NOT {
		if isLogical(x.X) {
			v := f.boolValue(x.X)
			return f.value(ir.Basic(ir.TBool), ir.Instr{Op: ir.OpNot, Args: []ir.Operand{v}})
		}
		v := f.expr(x.X)
		if c, ok := f.constOf(v); ok {
			return f.konst(boolConst(c.Text != "true"))
		}
		return f.value(ir.Basic(ir.TBool), ir.Instr{Op: ir.OpNot, Args: []ir.Operand{v}})
	}
	t := f.typeOf(x)
	v := f.expr(x.X)
	if c, ok := f.constOf(v); ok {
		if r, ok := foldNeg(c); ok {
			return f.konst(r)
		}
	}
	return f.value(irType(t), ir.Instr{Op: ir.MustLookup("neg." + arithSuffix(t)), Args: []ir.Operand{v}})
}

// isLogical reports whether an expression is evaluated through jumps (and, or).
func isLogical(x ast.Expr) bool {
	switch x := x.(type) {
	case *ast.ParenExpr:
		return isLogical(x.X)
	case *ast.BinaryExpr:
		return x.Op == token.AND || x.Op == token.OR
	}
	return false
}

func isNoneLit(x ast.Expr) bool {
	switch x := x.(type) {
	case *ast.ParenExpr:
		return isNoneLit(x.X)
	case *ast.BasicLit:
		return x.Kind == token.NONE
	}
	return false
}

func (f *fn) binary(x *ast.BinaryExpr) ir.Operand {
	switch x.Op {
	case token.AND, token.OR:
		return f.boolValue(x)
	case token.COALESCE:
		return f.coalesce(x)
	case token.EQ, token.NE:
		// A comparison with none checks for a missing value.
		if isNoneLit(x.X) || isNoneLit(x.Y) {
			other := x.X
			if isNoneLit(x.X) {
				other = x.Y
			}
			return f.expr(&ast.IsNoneExpr{X: other, Not: x.Op == token.NE})
		}
	}
	tx, ty := f.typeOf(x.X), f.typeOf(x.Y)
	a, b := f.expr(x.X), f.expr(x.Y)
	name, swap := binaryOp(x.Op, types.Unwrap(tx), types.Unwrap(ty))
	if swap {
		a, b = b, a
	}
	ca, okA := f.constOf(a)
	cb, okB := f.constOf(b)
	if okA && okB {
		if r, ok := fold(name, ca, cb); ok {
			return f.konst(r)
		}
	}
	return f.value(irType(f.typeOf(x)), ir.Instr{Op: ir.MustLookup(name), Args: []ir.Operand{a, b}})
}

// binaryOp returns the name of a typed operation; swap: the operands swap
// places (int * duration).
func binaryOp(op token.Kind, tx, ty types.Type) (string, bool) {
	switch op {
	case token.EQ:
		return "eq." + eqSuffix(tx, ty), false
	case token.NE:
		return "ne." + eqSuffix(tx, ty), false
	case token.LT:
		return "lt." + eqSuffix(tx, ty), false
	case token.LE:
		return "le." + eqSuffix(tx, ty), false
	case token.GT:
		return "gt." + eqSuffix(tx, ty), false
	case token.GE:
		return "ge." + eqSuffix(tx, ty), false
	}
	switch {
	case isBasic(tx, types.Timestamp) && isBasic(ty, types.Timestamp):
		return "diff.ts", false
	case isBasic(tx, types.Timestamp):
		if op == token.ADD {
			return "add.ts", false
		}
		return "sub.ts", false
	case isBasic(tx, types.Duration) && isBasic(ty, types.Duration):
		if op == token.ADD {
			return "add.dur", false
		}
		return "sub.dur", false
	case isBasic(tx, types.Duration):
		return "mul.dur", false
	case isBasic(ty, types.Duration):
		return "mul.dur", true
	case isBasic(tx, types.String):
		return "add.str", false
	}
	base := map[token.Kind]string{token.ADD: "add", token.SUB: "sub", token.MUL: "mul", token.QUO: "div", token.REM: "rem"}[op]
	return base + "." + arithSuffix(tx), false
}

// arithSuffix is the arithmetic suffix: i64, f64, dec or dur (unary minus).
func arithSuffix(t types.Type) string {
	switch {
	case isBasic(t, types.Float):
		return "f64"
	case isBasic(t, types.Decimal):
		return "dec"
	case isBasic(t, types.Duration):
		return "dur"
	}
	return "i64"
}

// eqSuffix is the comparison suffix by value kind (without "?").
func eqSuffix(tx, ty types.Type) string {
	t := tx
	if _, none := tx.(*types.None); none {
		t = ty
	}
	switch t := t.(type) {
	case *types.Basic:
		switch t.Kind {
		case types.Int, types.Bigint:
			return "i64"
		case types.Float:
			return "f64"
		case types.Decimal:
			return "dec"
		case types.String:
			return "str"
		case types.Bool:
			return "bool"
		case types.UUID:
			return "uuid"
		case types.Date:
			return "date"
		case types.Time:
			return "time"
		case types.Timestamp:
			return "ts"
		case types.Duration:
			return "dur"
		case types.Bytes:
			return "bytes"
		}
	case *types.Enum:
		return "enum"
	case *types.ID, *types.Ref:
		return "id"
	case *types.TimerID:
		return "timer"
	case *types.Value, *types.Record:
		return "rec"
	case *types.List:
		return "list"
	case *types.Set:
		return "set"
	case *types.Map:
		return "map"
	}
	panic(fmt.Sprintf("lower: comparison %v is not lowered", tx))
}

// coalesce is x ?? y: y is evaluated only if x is none.
func (f *fn) coalesce(x *ast.BinaryExpr) ir.Operand {
	res := f.temp(irType(f.typeOf(x)))
	v := f.expr(x.X)
	if c, ok := f.constOf(v); ok && !c.None {
		return v
	}
	none := f.value(ir.Basic(ir.TBool), ir.Instr{Op: ir.OpIsNone, Args: []ir.Operand{v}})
	some, other, done := f.newBlock(), f.newBlock(), f.newBlock()
	f.br(none, other, some)
	f.setBlock(some)
	f.emit(ir.Instr{Op: ir.OpMove, Dst: res, Args: []ir.Operand{v}})
	f.jmp(done)
	f.setBlock(other)
	f.emit(ir.Instr{Op: ir.OpMove, Dst: res, Args: []ir.Operand{f.expr(x.Y)}})
	f.jmp(done)
	f.setBlock(done)
	return ir.SlotOp(res)
}

func (f *fn) in(x *ast.InExpr) ir.Operand {
	v, coll := f.expr(x.X), f.expr(x.Y)
	op := ir.OpInList
	switch f.typeOf(x.Y).(type) {
	case *types.Set:
		op = ir.OpInSet
	case *types.Map:
		op = ir.OpInMap
	}
	r := f.value(ir.Basic(ir.TBool), ir.Instr{Op: op, Args: []ir.Operand{v, coll}})
	if x.Not {
		r = f.value(ir.Basic(ir.TBool), ir.Instr{Op: ir.OpNot, Args: []ir.Operand{r}})
	}
	return r
}

// selector is x.name, x?.name, an enum value, after.field.
func (f *fn) selector(x *ast.SelectorExpr) ir.Operand {
	if o := f.l.info.FeatureOf(x); o != nil {
		return f.ident(x.Sel)
	}
	if o := f.l.info.Uses[x.Sel]; o != nil && o.Kind == sema.ObjEnumValue {
		return f.konst(ir.Const{Type: &ir.Type{Kind: ir.TEnum, Name: o.Enum.Name}, Text: o.Name})
	}
	if id, ok := x.X.(*ast.Ident); ok {
		if o := f.l.info.Uses[id]; o != nil && o.Kind == sema.ObjBuiltin && o.Name == "after" {
			return f.value(irType(f.typeOf(x)), ir.Instr{Op: ir.OpAfterField, Sym: x.Sel.Name})
		}
	}
	t := irType(f.typeOf(x))
	recv := types.Unwrap(f.typeOf(x.X))
	get := func(v ir.Operand) ir.Operand {
		elem := t
		if x.Safe {
			// Under ?. it is the field's own type: it may be optional too.
			elem = irPropType(recv, x.Sel.Name, t)
		}
		switch r := recv.(type) {
		case *types.Value:
			return f.value(elem, ir.Instr{Op: ir.OpValGet, Sym: x.Sel.Name, Args: []ir.Operand{v}})
		case *types.Record:
			return f.value(elem, ir.Instr{Op: ir.OpRecGet, Sym: x.Sel.Name, Args: []ir.Operand{v}})
		default:
			return f.value(elem, ir.Instr{Op: ir.OpIntrinsic, Sym: recvKind(r) + "." + x.Sel.Name, Args: []ir.Operand{v}})
		}
	}
	var v ir.Operand
	switch recv.(type) {
	case *types.Value, *types.Record:
		v = f.unnarrowed(x.X) // rec.get and val.get accept an optional base too
	default:
		v = f.expr(x.X)
	}
	if !x.Safe {
		return get(v)
	}
	return f.safe(v, t, get)
}

// unnarrowed is the value of an expression without the copy into a
// narrowed slot (narrow) for variables and parameters.
func (f *fn) unnarrowed(x ast.Expr) ir.Operand {
	if id, ok := x.(*ast.Ident); ok {
		if o := f.l.info.Uses[id]; o != nil && (o.Kind == sema.ObjLocal || o.Kind == sema.ObjParam || o.Kind == sema.ObjFixture) {
			if s, ok := f.slots[o.Node]; ok {
				return ir.SlotOp(s)
			}
		}
	}
	return f.expr(x)
}

// irPropType is the type of a receiver property ignoring ?.
func irPropType(recv types.Type, name string, fallback *ir.Type) *ir.Type {
	switch r := recv.(type) {
	case *types.Value:
		if fl := r.Decl.Field(name); fl != nil {
			return irType(fl.Type)
		}
	case *types.Record:
		if name == "id" && r.Owner.ID != nil {
			return irType(r.Owner.ID)
		}
		if fl := r.Owner.Field(name); fl != nil {
			return irType(fl.Type)
		}
	}
	return fallback
}

// safe lowers x?.…: if x is none, the result is none.
func (f *fn) safe(v ir.Operand, t *ir.Type, then func(ir.Operand) ir.Operand) ir.Operand {
	res := f.temp(ir.Optional(t))
	none := f.value(ir.Basic(ir.TBool), ir.Instr{Op: ir.OpIsNone, Args: []ir.Operand{v}})
	some, isNone, done := f.newBlock(), f.newBlock(), f.newBlock()
	f.br(none, isNone, some)
	f.setBlock(some)
	f.emit(ir.Instr{Op: ir.OpMove, Dst: res, Args: []ir.Operand{then(v)}})
	f.jmp(done)
	f.setBlock(isNone)
	f.emit(ir.Instr{Op: ir.OpMove, Dst: res, Args: []ir.Operand{f.konst(ir.Const{Type: ir.Optional(t), None: true})}})
	f.jmp(done)
	f.setBlock(done)
	return ir.SlotOp(res)
}

// recvKind is the prefix of built-in properties and methods by receiver type.
func recvKind(t types.Type) string {
	switch t := t.(type) {
	case *types.Basic:
		switch t.Kind {
		case types.String:
			return "str"
		case types.Date:
			return "date"
		case types.Timestamp:
			return "ts"
		case types.Duration:
			return "dur"
		}
	case *types.List:
		return "list"
	case *types.Set:
		return "set"
	case *types.Map:
		return "map"
	case *types.Page:
		return "page"
	case *types.PageReq:
		return "pagereq"
	case *types.Caller:
		return "caller"
	}
	panic(fmt.Sprintf("lower: %v has no built-in members", t))
}

// bracket is [x] (an entity or record access) or a collection literal.
func (f *fn) bracket(x *ast.BracketExpr) ir.Operand {
	t := f.typeOf(x)
	if l, ok := types.Unwrap(t).(*types.List); ok && len(x.Elems) == 1 {
		if r, ok := l.Elem.(*types.Record); ok && r.Owner.Owner != nil {
			if _, ident := x.Elems[0].(*ast.Ident); ident {
				// [Stop]: the records of an inner entity of the aggregate.
				return f.value(irType(t), ir.Instr{Op: ir.OpInnerAll, Sym: r.Owner.Name})
			}
		}
	}
	switch u := types.Unwrap(t).(type) {
	case *types.Ref:
		return f.value(irType(t), ir.Instr{Op: ir.OpRefLoad, Sym: u.Owner.Name, Args: []ir.Operand{f.expr(x.Elems[0])}})
	case *types.Record:
		return f.value(irType(t), ir.Instr{Op: ir.OpRecLoad, Sym: u.Owner.Name, Args: []ir.Operand{f.expr(x.Elems[0])}})
	}
	var args []ir.Operand
	for _, e := range x.Elems {
		args = append(args, f.expr(e))
	}
	op := ir.OpCollList
	if _, ok := types.Unwrap(t).(*types.Set); ok {
		op = ir.OpCollSet
	}
	return f.value(irType(types.Unwrap(t)), ir.Instr{Op: op, Args: args})
}
