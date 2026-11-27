package lower

import (
	"math/big"
	"strconv"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ir"
)

// Constant folding (plan, §13.5): integers without overflow (int is 32
// bits), exact decimals, durations, string concatenation, comparisons.
// Division and floating-point numbers are not folded: the VM defines their
// semantics.

func fold(op string, a, b ir.Const) (ir.Const, bool) {
	if a.None || b.None {
		return ir.Const{}, false
	}
	base, kind, _ := strings.Cut(op, ".")
	switch kind {
	case "i64", "dur":
		x, err1 := strconv.ParseInt(a.Text, 10, 64)
		y, err2 := strconv.ParseInt(b.Text, 10, 64)
		if err1 != nil || err2 != nil {
			return ir.Const{}, false
		}
		if r, ok := cmpResult(base, compareInt(x, y)); ok {
			return boolConst(r), true
		}
		var z int64
		var ok bool
		switch base {
		case "add":
			z, ok = addInt(x, y)
		case "sub":
			z, ok = addInt(x, -y)
			ok = ok && y != -1<<63
		case "mul":
			z, ok = mulInt(x, y)
		}
		if !ok || a.Type.Kind == ir.TInt && (z < -1<<31 || z > 1<<31-1) {
			return ir.Const{}, false // overflow is a runtime failure; the VM reports it
		}
		return ir.Const{Type: a.Type, Text: strconv.FormatInt(z, 10)}, true
	case "dec":
		x, ok1 := new(big.Rat).SetString(a.Text)
		y, ok2 := new(big.Rat).SetString(b.Text)
		if !ok1 || !ok2 {
			return ir.Const{}, false
		}
		if r, ok := cmpResult(base, x.Cmp(y)); ok {
			return boolConst(r), true
		}
		z := new(big.Rat)
		switch base {
		case "add":
			z.Add(x, y)
		case "sub":
			z.Sub(x, y)
		case "mul":
			z.Mul(x, y)
		default:
			return ir.Const{}, false
		}
		text, ok := ir.RatText(z)
		if !ok {
			return ir.Const{}, false
		}
		return ir.Const{Type: a.Type, Text: text}, true
	case "str":
		switch base {
		case "add":
			return strConst(a.Text + b.Text), true
		case "eq", "ne":
			return boolConst((a.Text == b.Text) == (base == "eq")), true
		}
	case "bool", "enum":
		switch base {
		case "eq", "ne":
			return boolConst((a.Text == b.Text) == (base == "eq")), true
		}
	}
	return ir.Const{}, false
}

func compareInt(x, y int64) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}

// cmpResult is the result of a comparison by the sign of c; ok == false: not a comparison.
func cmpResult(base string, c int) (bool, bool) {
	switch base {
	case "eq":
		return c == 0, true
	case "ne":
		return c != 0, true
	case "lt":
		return c < 0, true
	case "le":
		return c <= 0, true
	case "gt":
		return c > 0, true
	case "ge":
		return c >= 0, true
	}
	return false, false
}

func addInt(x, y int64) (int64, bool) {
	z := x + y
	if (x > 0 && y > 0 && z < 0) || (x < 0 && y < 0 && z >= 0) {
		return 0, false
	}
	return z, true
}

func mulInt(x, y int64) (int64, bool) {
	if x == 0 || y == 0 {
		return 0, true
	}
	z := x * y
	if z/y != x || (x == -1 && y == -1<<63) || (y == -1 && x == -1<<63) {
		return 0, false
	}
	return z, true
}

// foldNeg folds a unary minus.
func foldNeg(c ir.Const) (ir.Const, bool) {
	if c.None {
		return ir.Const{}, false
	}
	switch c.Type.Kind {
	case ir.TInt, ir.TBigint, ir.TDuration:
		x, err := strconv.ParseInt(c.Text, 10, 64)
		if err != nil || x == -1<<63 || c.Type.Kind == ir.TInt && x == -1<<31 {
			return ir.Const{}, false
		}
		return ir.Const{Type: c.Type, Text: strconv.FormatInt(-x, 10)}, true
	case ir.TFloat:
		x, err := strconv.ParseFloat(c.Text, 64)
		if err != nil {
			return ir.Const{}, false
		}
		return ir.Const{Type: c.Type, Text: strconv.FormatFloat(-x, 'g', -1, 64)}, true
	case ir.TDecimal:
		x, ok := new(big.Rat).SetString(c.Text)
		if !ok {
			return ir.Const{}, false
		}
		text, _ := ir.RatText(x.Neg(x))
		return ir.Const{Type: c.Type, Text: text}, true
	}
	return ir.Const{}, false
}
