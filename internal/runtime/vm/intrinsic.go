package vm

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"unicode"
	"unicode/utf8"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
	"github.com/tycheg/reasonr-compiler/internal/runtime/dec"
)

// builtin is a built-in function of the standard library (RFC §8.3).
type builtin func(r *run, x []model.Value) (model.Value, error)

// builtins are the functions by number in bytecode.Intrinsics.
var builtins []builtin

// intrinsic calls built-in function id with arguments from registers.
func (r *run) intrinsic(id int32, args []uint32, regs []model.Value) (model.Value, error) {
	start := len(r.ibuf)
	sig := &bytecode.IntrinsicSigs[id]
	for i, a := range args {
		x := regs[a]
		k := sig.Args[min(i, len(sig.Args)-1)]
		if k != model.KindNone && x.Kind != k {
			r.ibuf = r.ibuf[:start]
			return model.None, fmt.Errorf("%w: %s: argument %d is %s", ErrUnsupported, bytecode.Intrinsics[id], i, x.Kind)
		}
		r.ibuf = append(r.ibuf, x)
	}
	res, err := builtins[id](r, r.ibuf[start:len(r.ibuf):len(r.ibuf)])
	r.ibuf = r.ibuf[:start]
	return res, err
}

func init() {
	fns := map[string]builtin{
		"string": func(r *run, x []model.Value) (model.Value, error) {
			s := r.a.StringStart()
			r.text(x[0])
			return r.a.StringEnd(s), nil
		},
		"str.length": func(r *run, x []model.Value) (model.Value, error) {
			return model.Int(int64(utf8.RuneCount(r.a.Data(x[0])))), nil
		},
		"str.trim": func(r *run, x []model.Value) (model.Value, error) {
			b := r.a.Data(x[0])
			start := len(b) - len(bytes.TrimLeftFunc(b, unicode.IsSpace))
			t := bytes.TrimSpace(b)
			return substr(x[0], start, len(t)), nil
		},
		"str.lower":      mapRunes(unicode.ToLower),
		"str.upper":      mapRunes(unicode.ToUpper),
		"str.startsWith": strTest(bytes.HasPrefix),
		"str.contains":   strTest(bytes.Contains),
		"str.matches": func(r *run, x []model.Value) (model.Value, error) {
			re, err := r.m.regexp(r.a.Text(x[1]))
			if err != nil {
				return model.None, err
			}
			return model.Bool(re.Match(r.a.Data(x[0]))), nil
		},
		"str.concat": func(r *run, x []model.Value) (model.Value, error) {
			s := r.a.StringStart()
			for _, p := range x {
				r.a.StringAppend(r.a.Data(p))
			}
			return r.a.StringEnd(s), nil
		},
		"ts.date": func(_ *run, x []model.Value) (model.Value, error) {
			return model.DateDays(floorDiv(int64(x[0].Lo), 86400) + epochDays), nil
		},
		"dur.seconds": func(_ *run, x []model.Value) (model.Value, error) {
			return model.Bigint(x[0].Int() / 1e9), nil
		},
		"date.addDays": func(_ *run, x []model.Value) (model.Value, error) {
			return model.DateDays(x[0].Days() + x[1].Int()), nil
		},
		"ts.addDays": func(_ *run, x []model.Value) (model.Value, error) {
			d, ok := mulOK(x[1].Int(), 86400)
			s, ok2 := addOK(int64(x[0].Lo), d)
			if !ok || !ok2 {
				return model.None, ErrOverflow
			}
			v := x[0]
			v.Lo = uint64(s)
			return v, nil
		},
		"date.between": between,
		"ts.between":   between,
		"list.at": func(r *run, x []model.Value) (model.Value, error) {
			i := x[1].Int()
			if i < 0 || i >= int64(x[0].Ref.Len) {
				return model.None, nil
			}
			return r.a.Elems(x[0])[i], nil
		},
		"map.count":   count,
		"map.isEmpty": isEmpty,
		"map.keys": func(r *run, x []model.Value) (model.Value, error) {
			return r.half(x[0], 0), nil
		},
		"map.values": func(r *run, x []model.Value) (model.Value, error) {
			return r.half(x[0], 1), nil
		},
		"map.get": func(r *run, x []model.Value) (model.Value, error) {
			if i := indexIn(r.a, x[0], x[1], 2); i >= 0 {
				return r.a.Elems(x[0])[i+1], nil
			}
			return model.None, nil
		},
		"map.put": func(r *run, x []model.Value) (model.Value, error) {
			return r.put(x[0], x[1], x[2]), nil
		},
		"map.contains": func(r *run, x []model.Value) (model.Value, error) {
			return model.Bool(indexIn(r.a, x[0], x[1], 2) >= 0), nil
		},
		"page.items":     elem(0),
		"page.next":      elem(1),
		"pagereq.size":   elem(0),
		"pagereq.cursor": elem(1),
		"caller.roles": func(r *run, x []model.Value) (model.Value, error) {
			return r.a.Elems(x[0])[0], nil
		},
	}
	for _, coll := range []string{"list", "set"} {
		fns[coll+".count"] = count
		fns[coll+".isEmpty"] = isEmpty
		fns[coll+".first"] = func(r *run, x []model.Value) (model.Value, error) {
			if s := r.a.Elems(x[0]); len(s) > 0 {
				return s[0], nil
			}
			return model.None, nil
		}
		fns[coll+".last"] = func(r *run, x []model.Value) (model.Value, error) {
			if s := r.a.Elems(x[0]); len(s) > 0 {
				return s[len(s)-1], nil
			}
			return model.None, nil
		}
		fns[coll+".contains"] = func(r *run, x []model.Value) (model.Value, error) {
			return model.Bool(indexIn(r.a, x[0], x[1], 1) >= 0), nil
		}
		fns[coll+".with"] = func(r *run, x []model.Value) (model.Value, error) { return r.with(x[0], x[1]), nil }
		fns[coll+".without"] = func(r *run, x []model.Value) (model.Value, error) { return r.without(x[0], x[1]), nil }
		fns[coll+".sum.i32"] = sum(i32(func(a, b int64) int64 { return a + b }), model.Int(0))
		fns[coll+".sum.i64"] = sum(i64Add, model.Bigint(0))
		fns[coll+".sum.f64"] = sum(f64(func(a, b float64) float64 { return a + b }), model.Float(0))
		fns[coll+".sum.dec"] = sum(decOp(dec.Dec.Add), model.Decimal(dec.Dec{}))
	}
	numeric(fns)
	scenario(fns)
	builtins = make([]builtin, len(bytecode.Intrinsics))
	for i, name := range bytecode.Intrinsics {
		f, ok := fns[name]
		if !ok {
			panic("vm: built-in function not implemented: " + name)
		}
		builtins[i] = f
	}
}

func count(r *run, x []model.Value) (model.Value, error) {
	return model.Int(int64(r.a.Len(x[0]))), nil
}

func isEmpty(_ *run, x []model.Value) (model.Value, error) {
	return model.Bool(x[0].Ref.Len == 0), nil
}

func elem(i int) builtin {
	return func(r *run, x []model.Value) (model.Value, error) { return r.a.Elems(x[0])[i], nil }
}

func between(r *run, x []model.Value) (model.Value, error) {
	return model.Bool(r.a.Compare(x[1], x[0]) <= 0 && r.a.Compare(x[0], x[2]) <= 0), nil
}

func sum(add typedOp, zero model.Value) builtin {
	return func(r *run, x []model.Value) (model.Value, error) {
		acc := zero
		for _, e := range r.a.Elems(x[0]) {
			var err error
			if acc, err = add(r, acc, e); err != nil {
				return model.None, err
			}
		}
		return acc, nil
	}
}

func strTest(f func(s, sub []byte) bool) builtin {
	return func(r *run, x []model.Value) (model.Value, error) {
		return model.Bool(f(r.a.Data(x[0]), r.a.Data(x[1]))), nil
	}
}

// substr is a part of a string without copying: a reference into the same bytes.
func substr(v model.Value, start, n int) model.Value {
	if n == 0 {
		v.Ref = model.ArenaRef{}
		return v
	}
	v.Ref.Off += uint32(start)
	v.Ref.Len = uint32(n)
	return v
}

func mapRunes(f func(rune) rune) builtin {
	return func(r *run, x []model.Value) (model.Value, error) {
		src := r.a.Data(x[0])
		s := r.a.StringStart()
		var buf [utf8.UTFMax]byte
		for len(src) > 0 {
			c, n := utf8.DecodeRune(src)
			src = src[n:]
			r.a.StringAppend(buf[:utf8.EncodeRune(buf[:], f(c))])
		}
		return r.a.StringEnd(s), nil
	}
}

// numeric holds the conversions and numeric functions by argument kind.
func numeric(fns map[string]builtin) {
	toInt := func(bits int, mk func(int64) model.Value) map[string]builtin {
		limit := math.Ldexp(1, bits-1)
		fit := func(v int64) (model.Value, error) {
			if bits == 32 && v != int64(int32(v)) {
				return model.None, fmt.Errorf("%w: %d out of int range", ErrOverflow, v)
			}
			return mk(v), nil
		}
		return map[string]builtin{
			"i32": func(_ *run, x []model.Value) (model.Value, error) { return fit(x[0].Int()) },
			"i64": func(_ *run, x []model.Value) (model.Value, error) { return fit(x[0].Int()) },
			"f64": func(_ *run, x []model.Value) (model.Value, error) {
				f := math.Trunc(x[0].Float())
				if math.IsNaN(f) || f >= limit || f < -limit {
					return model.None, fmt.Errorf("%w: %v", ErrOverflow, x[0].Float())
				}
				return fit(int64(f))
			},
			"dec": func(_ *run, x []model.Value) (model.Value, error) {
				v, ok := x[0].Decimal().Int64()
				if !ok {
					return model.None, ErrOverflow
				}
				return fit(v)
			},
			"str": func(r *run, x []model.Value) (model.Value, error) {
				v, err := strconv.ParseInt(string(r.a.Data(x[0])), 10, bits)
				if err != nil {
					return model.None, errNotNumber
				}
				return mk(v), nil
			},
		}
	}
	add := func(to string, m map[string]builtin) {
		for k, f := range m {
			fns[to+"."+k] = f
		}
	}
	add("int", toInt(32, model.Int))
	add("bigint", toInt(64, model.Bigint))
	floatOf := func(f float64) (model.Value, error) { return model.Float(f), nil }
	add("float", map[string]builtin{
		"i32": func(_ *run, x []model.Value) (model.Value, error) { return floatOf(float64(x[0].Int())) },
		"i64": func(_ *run, x []model.Value) (model.Value, error) { return floatOf(float64(x[0].Int())) },
		"f64": func(_ *run, x []model.Value) (model.Value, error) { return x[0], nil },
		"dec": func(_ *run, x []model.Value) (model.Value, error) { return floatOf(x[0].Decimal().Float64()) },
		"str": func(r *run, x []model.Value) (model.Value, error) {
			f, err := strconv.ParseFloat(string(r.a.Data(x[0])), 64)
			if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
				return model.None, errNotNumber
			}
			return floatOf(f)
		},
	})
	add("decimal", map[string]builtin{
		"i32": func(_ *run, x []model.Value) (model.Value, error) {
			return model.Decimal(dec.FromInt(x[0].Int())), nil
		},
		"i64": func(_ *run, x []model.Value) (model.Value, error) {
			return model.Decimal(dec.FromInt(x[0].Int())), nil
		},
		"f64": func(_ *run, x []model.Value) (model.Value, error) {
			d, ok := dec.FromFloat(x[0].Float())
			if !ok {
				return model.None, fmt.Errorf("%w: %v", ErrOverflow, x[0].Float())
			}
			return model.Decimal(d), nil
		},
		"dec": func(_ *run, x []model.Value) (model.Value, error) { return x[0], nil },
		"str": func(r *run, x []model.Value) (model.Value, error) {
			d, err := dec.Parse(string(r.a.Data(x[0])))
			if err != nil {
				return model.None, errNotNumber
			}
			return model.Decimal(d), nil
		},
	})
	ident := func(_ *run, x []model.Value) (model.Value, error) { return x[0], nil }
	for _, k := range []string{"i32", "i64"} {
		neg := typedOps[mustOp("neg."+k)]
		lt := typedOps[mustOp("lt."+k)]
		fns["abs."+k] = func(r *run, x []model.Value) (model.Value, error) {
			if x[0].Int() < 0 {
				return neg(r, x[0], model.None)
			}
			return x[0], nil
		}
		fns["floor."+k], fns["ceil."+k] = ident, ident
		fns["min."+k] = pick(lt, true)
		fns["max."+k] = pick(lt, false)
		fns["round."+k] = roundInt(k == "i32")
	}
	fns["abs.f64"] = func(_ *run, x []model.Value) (model.Value, error) {
		return model.Float(math.Abs(x[0].Float())), nil
	}
	fns["floor.f64"] = func(_ *run, x []model.Value) (model.Value, error) {
		return model.Float(math.Floor(x[0].Float())), nil
	}
	fns["ceil.f64"] = func(_ *run, x []model.Value) (model.Value, error) {
		return model.Float(math.Ceil(x[0].Float())), nil
	}
	fns["min.f64"] = func(_ *run, x []model.Value) (model.Value, error) {
		return model.Float(min(x[0].Float(), x[1].Float())), nil
	}
	fns["max.f64"] = func(_ *run, x []model.Value) (model.Value, error) {
		return model.Float(max(x[0].Float(), x[1].Float())), nil
	}
	fns["round.f64"] = func(_ *run, x []model.Value) (model.Value, error) {
		p := math.Pow10(int(x[1].Int()))
		v := math.Round(x[0].Float()*p) / p
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return x[0], nil // more precise than float: the value does not change
		}
		return model.Float(v), nil
	}
	decFn := func(f func(d dec.Dec) (dec.Dec, bool)) builtin {
		return func(_ *run, x []model.Value) (model.Value, error) {
			d, ok := f(x[0].Decimal())
			if !ok {
				return model.None, ErrOverflow
			}
			return model.Decimal(d), nil
		}
	}
	fns["abs.dec"] = decFn(func(d dec.Dec) (dec.Dec, bool) { return d.Abs(), true })
	fns["floor.dec"] = decFn(dec.Dec.Floor)
	fns["ceil.dec"] = decFn(dec.Dec.Ceil)
	lt := typedOps[mustOp("lt.dec")]
	fns["min.dec"] = pick(lt, true)
	fns["max.dec"] = pick(lt, false)
	fns["round.dec"] = func(_ *run, x []model.Value) (model.Value, error) {
		d, ok := x[0].Decimal().Round(int(x[1].Int()))
		if !ok {
			return model.None, ErrOverflow
		}
		return model.Decimal(d), nil
	}
}

func mustOp(name string) bytecode.Op {
	op, ok := bytecode.Lookup(name)
	if !ok {
		panic("vm: no operation " + name)
	}
	return op
}

// pick is min (less) or max by the "less" operation.
func pick(lt typedOp, less bool) builtin {
	return func(r *run, x []model.Value) (model.Value, error) {
		b, _ := lt(r, x[0], x[1])
		if b.Bool() == less {
			return x[0], nil
		}
		return x[1], nil
	}
}

// roundInt is round(x, n) for integers: n ≥ 0 does not change the number,
// n < 0 rounds to tens, hundreds… away from zero.
func roundInt(i32 bool) builtin {
	return func(_ *run, x []model.Value) (model.Value, error) {
		v, n := x[0].Int(), x[1].Int()
		mk := model.Bigint
		if i32 {
			mk = model.Int
		}
		if n >= 0 {
			return x[0], nil
		}
		if n < -18 {
			if v == 0 {
				return mk(0), nil
			}
			// |v| < 10^19 ≤ half of 10^(-n) for n ≤ -20; for n = -19 rounding up overflows.
			if n == -19 && (v >= 5e18 || v <= -5e18) {
				return model.None, ErrOverflow
			}
			return mk(0), nil
		}
		p := int64(1)
		for range -n {
			p *= 10
		}
		q, rem := v/p, v%p
		if rem >= p-rem && rem > 0 {
			q++
		} else if -rem >= p+rem && rem < 0 {
			q--
		}
		res, ok := mulOK(q, p)
		if !ok || i32 && res != int64(int32(res)) {
			return model.None, ErrOverflow
		}
		return mk(res), nil
	}
}

// errNotNumber: a string does not convert to a number. The message does not
// contain the string itself: it goes into the incident log, and the string
// may be personal (decision 15 of RFC §20).
var errNotNumber = fmt.Errorf("%w: the string is not a number", ErrConvert)
