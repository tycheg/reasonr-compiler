package vm

import (
	"bytes"
	"cmp"
	"math"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
	"github.com/tycheg/reasonr-compiler/internal/runtime/dec"
)

// typedOp is a typed operation: A = f(B, C); unary ones do not read C.
type typedOp func(r *run, x, y model.Value) (model.Value, error)

// typedOps are the typed operations by code.
var typedOps [256]typedOp

func init() {
	ops := map[string]typedOp{
		"add.i32": i32(func(x, y int64) int64 { return x + y }),
		"sub.i32": i32(func(x, y int64) int64 { return x - y }),
		"mul.i32": i32(func(x, y int64) int64 { return x * y }),
		"div.i32": i32div(func(x, y int64) int64 { return x / y }),
		"rem.i32": i32div(func(x, y int64) int64 { return x % y }),
		"neg.i32": i32(func(x, _ int64) int64 { return -x }),
		"add.i64": i64Add, "sub.i64": i64Sub, "mul.i64": i64Mul, "div.i64": i64Div, "rem.i64": i64Rem, "neg.i64": i64Neg,
		"add.f64": f64(func(x, y float64) float64 { return x + y }),
		"sub.f64": f64(func(x, y float64) float64 { return x - y }),
		"mul.f64": f64(func(x, y float64) float64 { return x * y }),
		"div.f64": f64div(func(x, y float64) float64 { return x / y }),
		"rem.f64": f64div(math.Mod),
		"neg.f64": f64(func(x, _ float64) float64 { return -x }),
		"add.dec": decOp(dec.Dec.Add), "sub.dec": decOp(dec.Dec.Sub), "mul.dec": decOp(dec.Dec.Mul),
		"div.dec": decOp(dec.Dec.Quo), "rem.dec": decOp(dec.Dec.Rem),
		"neg.dec": func(_ *run, x, _ model.Value) (model.Value, error) {
			return model.Decimal(x.Decimal().Neg()), nil
		},
		"add.str": func(r *run, x, y model.Value) (model.Value, error) {
			s := r.a.StringStart()
			r.a.StringAppend(r.a.Data(x))
			r.a.StringAppend(r.a.Data(y))
			return r.a.StringEnd(s), nil
		},
		"add.dur": durOp(func(x, y int64) (int64, bool) { return addOK(x, y) }),
		"sub.dur": durOp(func(x, y int64) (int64, bool) { return subOK(x, y) }),
		"mul.dur": durOp(func(x, y int64) (int64, bool) { return mulOK(x, y) }),
		"neg.dur": durOp(func(x, _ int64) (int64, bool) { return subOK(0, x) }),
		"add.ts":  tsShift(1),
		"sub.ts":  tsShift(-1),
		"diff.ts": tsDiff,
	}
	for _, base := range []string{"eq", "ne"} {
		for _, k := range bytecode.EqKinds {
			ops[base+"."+k] = equality(base == "ne")
		}
	}
	for _, base := range []string{"lt", "le", "gt", "ge"} {
		test := map[string]func(int) bool{
			"lt": func(c int) bool { return c < 0 }, "le": func(c int) bool { return c <= 0 },
			"gt": func(c int) bool { return c > 0 }, "ge": func(c int) bool { return c >= 0 },
		}[base]
		for _, k := range bytecode.OrdKinds {
			if k != "f64" {
				ops[base+"."+k] = order(k, test)
			}
		}
		ops[base+".f64"] = floatOrder(base)
	}
	for name, f := range ops {
		op, ok := bytecode.Lookup(name)
		if !ok {
			panic("vm: no operation " + name)
		}
		typedOps[op] = f
	}
	for op := bytecode.NumFixedOps(); op < bytecode.NumOps(); op++ {
		if typedOps[op] == nil {
			panic("vm: operation not implemented: " + bytecode.Op(op).String())
		}
	}
}

func i32(f func(x, y int64) int64) typedOp {
	return func(_ *run, x, y model.Value) (model.Value, error) {
		v := f(x.Int(), y.Int())
		if v != int64(int32(v)) {
			return model.None, ErrOverflow
		}
		return model.Int(v), nil
	}
}

func i32div(f func(x, y int64) int64) typedOp {
	return func(r *run, x, y model.Value) (model.Value, error) {
		if y.Int() == 0 {
			return model.None, ErrDivZero
		}
		return i32(f)(r, x, y)
	}
}

func addOK(x, y int64) (int64, bool) {
	s := x + y
	return s, (x^s)&(y^s) >= 0
}

func subOK(x, y int64) (int64, bool) {
	s := x - y
	return s, (x^y)&(x^s) >= 0
}

func mulOK(x, y int64) (int64, bool) {
	if x == 0 || y == 0 {
		return 0, true
	}
	p := x * y
	return p, p/y == x && !(x == -1 && y == math.MinInt64) && !(y == -1 && x == math.MinInt64)
}

func i64(f func(x, y int64) (int64, bool)) typedOp {
	return func(_ *run, x, y model.Value) (model.Value, error) {
		v, ok := f(x.Int(), y.Int())
		if !ok {
			return model.None, ErrOverflow
		}
		return model.Bigint(v), nil
	}
}

var (
	i64Add = i64(addOK)
	i64Sub = i64(subOK)
	i64Mul = i64(mulOK)
	i64Neg = i64(func(x, _ int64) (int64, bool) { return subOK(0, x) })
	i64Div = i64divide(func(x, y int64) int64 { return x / y })
	i64Rem = i64divide(func(x, y int64) int64 { return x % y })
)

func i64divide(f func(x, y int64) int64) typedOp {
	return func(_ *run, x, y model.Value) (model.Value, error) {
		switch {
		case y.Int() == 0:
			return model.None, ErrDivZero
		case x.Int() == math.MinInt64 && y.Int() == -1:
			return model.None, ErrOverflow
		}
		return model.Bigint(f(x.Int(), y.Int())), nil
	}
}

// f64 is float arithmetic: an infinity or NaN in the result is an overflow.
func f64(f func(x, y float64) float64) typedOp {
	return func(_ *run, x, y model.Value) (model.Value, error) {
		v := f(x.Float(), y.Float())
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return model.None, ErrOverflow
		}
		return model.Float(v), nil
	}
}

func f64div(f func(x, y float64) float64) typedOp {
	return func(r *run, x, y model.Value) (model.Value, error) {
		if y.Float() == 0 {
			return model.None, ErrDivZero
		}
		return f64(f)(r, x, y)
	}
}

func decOp(f func(x, y dec.Dec) (dec.Dec, bool)) typedOp {
	return func(_ *run, x, y model.Value) (model.Value, error) {
		d, ok := f(x.Decimal(), y.Decimal())
		if !ok {
			if y.Decimal().IsZero() {
				return model.None, ErrDivZero // Quo and Rem: no other errors are possible with zero
			}
			return model.None, ErrOverflow
		}
		return model.Decimal(d), nil
	}
}

func durOp(f func(x, y int64) (int64, bool)) typedOp {
	return func(_ *run, x, y model.Value) (model.Value, error) {
		v, ok := f(x.Int(), y.Int())
		if !ok {
			return model.None, ErrOverflow
		}
		return model.Value{Kind: model.KindDuration, Lo: uint64(v)}, nil
	}
}

// tsShift is an instant ± a duration.
func tsShift(sign int64) typedOp {
	return func(_ *run, t, d model.Value) (model.Value, error) {
		ns := d.Int()
		sec, nsec := ns/1e9, ns%1e9
		if sign < 0 {
			sec, nsec = -sec, -nsec
		}
		s, ok := addOK(int64(t.Lo), sec)
		n := int64(t.Hi) + nsec
		switch {
		case n < 0:
			n += 1e9
			s, ok = subOK(s, 1)
		case n >= 1e9:
			n -= 1e9
			s, ok = addOK(s, 1)
		}
		if !ok {
			return model.None, ErrOverflow
		}
		return model.Value{Kind: model.KindTimestamp, Lo: uint64(s), Hi: uint64(n)}, nil
	}
}

// tsDiff is the difference of instants: a duration.
func tsDiff(_ *run, x, y model.Value) (model.Value, error) {
	ds, ok1 := subOK(int64(x.Lo), int64(y.Lo))
	ns, ok2 := mulOK(ds, 1e9)
	v, ok3 := addOK(ns, int64(x.Hi)-int64(y.Hi))
	if !ok1 || !ok2 || !ok3 {
		return model.None, ErrOverflow
	}
	return model.Value{Kind: model.KindDuration, Lo: uint64(v)}, nil
}

// equality is eq and ne: structural equality (RFC §7.5); none equals only none.
func equality(neg bool) typedOp {
	return func(r *run, x, y model.Value) (model.Value, error) {
		return model.Bool(r.a.Equal(x, y) != neg), nil
	}
}

// order compares ordered kinds.
func order(kind string, test func(int) bool) typedOp {
	var c func(r *run, x, y model.Value) int
	switch kind {
	case "i32", "i64", "date", "time", "dur":
		c = func(_ *run, x, y model.Value) int { return cmp.Compare(x.Int(), y.Int()) }
	case "dec":
		c = func(_ *run, x, y model.Value) int { return x.Decimal().Cmp(y.Decimal()) }
	case "str":
		c = func(r *run, x, y model.Value) int { return bytes.Compare(r.a.Data(x), r.a.Data(y)) }
	case "ts":
		c = func(r *run, x, y model.Value) int { return r.a.Compare(x, y) }
	default:
		panic("vm: comparison " + kind)
	}
	return func(r *run, x, y model.Value) (model.Value, error) {
		return model.Bool(test(c(r, x, y))), nil
	}
}

// floatOrder compares floats per IEEE 754: any comparison with NaN is false.
func floatOrder(base string) typedOp {
	return func(_ *run, x, y model.Value) (model.Value, error) {
		a, b := x.Float(), y.Float()
		switch base {
		case "lt":
			return model.Bool(a < b), nil
		case "le":
			return model.Bool(a <= b), nil
		case "gt":
			return model.Bool(a > b), nil
		}
		return model.Bool(a >= b), nil
	}
}
