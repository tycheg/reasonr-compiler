package vm

import (
	"context"
	"errors"
	"math"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
	"github.com/tycheg/reasonr-compiler/internal/runtime/dec"
)

// opRun is the state for calling operations directly: a taxi machine (the
// schema with enums and records is needed for printing).
func opRun(t *testing.T) (*fixture, *run) {
	f := load(t, example("taxi"), "")
	r := f.m.start(context.Background(), f.env, model.Caller{Roles: []string{"admin", "support"}}, nil, f.now)
	return f, r
}

func (r *run) textOf(v model.Value) string {
	s := r.a.StringStart()
	r.text(v)
	return r.a.Text(r.a.StringEnd(s))
}

func (r *run) coll(kind model.Kind, xs ...model.Value) model.Value {
	m := r.a.Mark()
	for _, x := range xs {
		r.a.Push(x)
	}
	return r.a.End(kind, 0, m)
}

func TestTypedOps(t *testing.T) {
	f, r := opRun(t)
	a := r.a
	ts := func(sec, ns int64) model.Value { return model.Timestamp(time.Unix(sec, ns)) }
	dur := func(d time.Duration) model.Value { return model.Duration(d) }
	i32, i64 := model.Int, model.Bigint
	tested := map[string]bool{}
	for _, tc := range []struct {
		op   string
		x, y model.Value
		want string // an error is "!overflow" and so on
	}{
		{"add.i32", i32(1), i32(2), "3"},
		{"add.i32", i32(math.MaxInt32), i32(1), "!overflow"},
		{"sub.i32", i32(math.MinInt32), i32(1), "!overflow"},
		{"mul.i32", i32(1 << 16), i32(1 << 16), "!overflow"},
		{"div.i32", i32(7), i32(2), "3"},
		{"div.i32", i32(7), i32(0), "!division by zero"},
		{"div.i32", i32(math.MinInt32), i32(-1), "!overflow"},
		{"rem.i32", i32(-7), i32(3), "-1"},
		{"rem.i32", i32(1), i32(0), "!division by zero"},
		{"neg.i32", i32(math.MinInt32), model.None, "!overflow"},
		{"add.i64", i64(math.MaxInt64), i64(1), "!overflow"},
		{"add.i64", i64(1 << 40), i64(1), "1099511627777"},
		{"sub.i64", i64(math.MinInt64), i64(1), "!overflow"},
		{"mul.i64", i64(1 << 32), i64(1 << 32), "!overflow"},
		{"mul.i64", i64(-1), i64(math.MinInt64), "!overflow"},
		{"mul.i64", i64(-3), i64(4), "-12"},
		{"div.i64", i64(math.MinInt64), i64(-1), "!overflow"},
		{"div.i64", i64(1), i64(0), "!division by zero"},
		{"rem.i64", i64(7), i64(-3), "1"},
		{"neg.i64", i64(math.MinInt64), model.None, "!overflow"},
		{"add.f64", model.Float(1.5), model.Float(2), "3.5"},
		{"sub.f64", model.Float(1), model.Float(1.5), "-0.5"},
		{"mul.f64", model.Float(math.MaxFloat64), model.Float(2), "!overflow"},
		{"div.f64", model.Float(1), model.Float(0), "!division by zero"},
		{"rem.f64", model.Float(7.5), model.Float(2), "1.5"},
		{"neg.f64", model.Float(2), model.None, "-2"},
		{"add.dec", decimal("0.1"), decimal("0.2"), "0.3"},
		{"sub.dec", decimal("0.1"), decimal("0.3"), "-0.2"},
		{"mul.dec", decimal("1e19"[:0] + "10000000000"), decimal("10000000000"), "!overflow"},
		{"div.dec", decimal("1"), decimal("3"), "0.333333333333333333"},
		{"div.dec", decimal("1"), decimal("0"), "!division by zero"},
		{"rem.dec", decimal("7.5"), decimal("2"), "1.5"},
		{"neg.dec", decimal("2.5"), model.None, "-2.5"},
		{"add.str", a.String("ab"), a.String("γδ"), "abγδ"},
		{"add.dur", dur(time.Minute), dur(30 * time.Second), "90s"},
		{"sub.dur", dur(time.Minute), dur(2 * time.Minute), "-1m"},
		{"mul.dur", dur(90 * time.Second), i32(2), "3m"},
		{"mul.dur", dur(math.MaxInt64), i64(2), "!overflow"},
		{"neg.dur", dur(time.Hour), model.None, "-1h"},
		{"add.ts", ts(10, 900_000_000), dur(1500 * time.Millisecond), "1970-01-01T00:00:12.4Z"},
		{"sub.ts", ts(10, 100_000_000), dur(1500 * time.Millisecond), "1970-01-01T00:00:08.6Z"},
		{"diff.ts", ts(10, 100_000_000), ts(8, 600_000_000), "1500ms"},
		{"diff.ts", model.Timestamp(time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC)), ts(1<<40, 0), "!overflow"},
	} {
		tested[tc.op] = true
		op, _ := bytecode.Lookup(tc.op)
		v, err := typedOps[op](r, tc.x, tc.y)
		got := ""
		if err != nil {
			got = "!" + err.Error()
		} else {
			got = r.textOf(v)
		}
		if got != tc.want {
			t.Errorf("%s(%s, %s) = %s, expected %s", tc.op, r.textOf(tc.x), r.textOf(tc.y), got, tc.want)
		}
	}

	// Comparisons: pairs x < y of every kind.
	enum := f.enum("Ordering.Currency", "rub")
	pairs := map[string][2]model.Value{
		"i32": {i32(1), i32(2)}, "i64": {i64(-5), i64(5)}, "f64": {model.Float(1), model.Float(1.5)},
		"dec": {decimal("-1"), decimal("0.5")}, "str": {a.String("a"), a.String("b")},
		"bool": {model.Bool(false), model.Bool(true)}, "uuid": {model.UUID([16]byte{1}), model.UUID([16]byte{2})},
		"date": {model.Date(2026, 1, 1), model.Date(2026, 1, 2)}, "time": {model.TimeOfDay(time.Hour), model.TimeOfDay(2 * time.Hour)},
		"ts": {ts(1, 5), ts(1, 6)}, "dur": {dur(-time.Second), dur(time.Second)}, "bytes": {a.Bytes([]byte{1}), a.Bytes([]byte{2})},
		"enum": {enum, model.Enum(enum.Type, 5)}, "id": {f.id("Order", 1), f.id("Order", 2)},
		"timer": {model.TimerID([16]byte{1}), model.TimerID([16]byte{2})},
		"rec":   {a.Record(1, i32(1)).Value(), a.Record(1, i32(2)).Value()},
		"list":  {r.coll(model.KindList, i32(1), i32(2)), r.coll(model.KindList, i32(2), i32(1))},
		"set":   {r.coll(model.KindSet, i32(1)), r.coll(model.KindSet, i32(1), i32(2))},
		"map":   {r.coll(model.KindMap, i32(1), i32(2)), r.coll(model.KindMap, i32(1), i32(3))},
	}
	call := func(name string, x, y model.Value) bool {
		tested[name] = true
		op, ok := bytecode.Lookup(name)
		if !ok {
			t.Fatalf("no operation %s", name)
		}
		v, err := typedOps[op](r, x, y)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		return v.Bool()
	}
	for _, k := range bytecode.EqKinds {
		x, y := pairs[k][0], pairs[k][1]
		if !call("eq."+k, x, x) || call("eq."+k, x, y) || call("ne."+k, x, x) || !call("ne."+k, x, y) ||
			call("eq."+k, x, model.None) || !call("ne."+k, model.None, y) || !call("eq."+k, model.None, model.None) {
			t.Errorf("equality %s", k)
		}
	}
	setA := r.coll(model.KindSet, i32(1), i32(2))
	setB := r.coll(model.KindSet, i32(2), i32(1))
	if !call("eq.set", setA, setB) {
		t.Error("sets in a different order are not equal")
	}
	for _, k := range bytecode.OrdKinds {
		x, y := pairs[k][0], pairs[k][1]
		if !call("lt."+k, x, y) || call("lt."+k, y, x) || !call("le."+k, x, x) || !call("gt."+k, y, x) ||
			call("ge."+k, x, y) || !call("ge."+k, y, y) || call("le."+k, y, x) || call("gt."+k, x, x) {
			t.Errorf("order %s", k)
		}
	}
	nan := model.Float(math.NaN())
	if call("lt.f64", nan, model.Float(1)) || call("ge.f64", nan, nan) {
		t.Error("a comparison with NaN is true")
	}
	for op := bytecode.NumFixedOps(); op < bytecode.NumOps(); op++ {
		if name := bytecode.Op(op).String(); !tested[name] {
			t.Errorf("operation %s is not checked", name)
		}
	}
}

func TestIntrinsics(t *testing.T) {
	f, r := opRun(t)
	a := r.a
	i32, i64, fl := model.Int, model.Bigint, model.Float
	s := a.String
	list := func(xs ...model.Value) model.Value { return r.coll(model.KindList, xs...) }
	set := func(xs ...model.Value) model.Value { return r.coll(model.KindSet, xs...) }
	mp := func(xs ...model.Value) model.Value { return r.coll(model.KindMap, xs...) }
	ts := model.Timestamp(time.Date(2026, 10, 4, 23, 30, 0, 0, time.UTC))
	page := r.coll(model.KindPage, list(i32(1)), s("next"))
	type tc struct {
		name string
		args []model.Value
		want string
	}
	cases := []tc{
		{"string", []model.Value{decimal("1.50")}, "1.5"},
		{"string", []model.Value{f.enum("Ordering.Currency", "rub")}, "rub"},
		{"string", []model.Value{model.Duration(90 * time.Second)}, "90s"},
		{"string", []model.Value{mp(s("k"), list(i32(1), model.None))}, "[k: [1, none]]"},
		{"string", []model.Value{model.TimeOfDay(9*time.Hour + 5*time.Minute)}, "09:05"},
		{"string", []model.Value{a.ID(f.typ("Order"), s("A-1"))}, "A-1"},
		{"str.length", []model.Value{s("δέλτα!")}, "6"},
		{"str.trim", []model.Value{s("  yes \n")}, "yes"},
		{"str.trim", []model.Value{s("   ")}, ""},
		{"str.lower", []model.Value{s("ΔέΛτΑ")}, "δέλτα"},
		{"str.upper", []model.Value{s("abc")}, "ABC"},
		{"str.startsWith", []model.Value{s("+7999"), s("+7")}, "true"},
		{"str.contains", []model.Value{s("abc"), s("d")}, "false"},
		{"str.matches", []model.Value{s("+79990000000"), s(`\+7\d{10}`)}, "true"},
		{"str.matches", []model.Value{s("x+79990000000"), s(`\+7\d{10}`)}, "false"},
		{"str.matches", []model.Value{s("x"), s(`(`)}, "!invalid regular expression"},
		{"str.concat", []model.Value{s("a"), s("β"), s("c")}, "aβc"},
		{"ts.date", []model.Value{ts}, "2026-10-04"},
		{"ts.date", []model.Value{model.Timestamp(time.Unix(-1, 0))}, "1969-12-31"},
		{"dur.seconds", []model.Value{model.Duration(90500 * time.Millisecond)}, "90"},
		{"date.addDays", []model.Value{model.Date(2026, 12, 31), i32(1)}, "2027-01-01"},
		{"ts.addDays", []model.Value{ts, i32(-1)}, "2026-10-03T23:30:00Z"},
		{"date.between", []model.Value{model.Date(2026, 1, 2), model.Date(2026, 1, 1), model.Date(2026, 1, 2)}, "true"},
		{"ts.between", []model.Value{ts, ts, ts}, "true"},
		{"list.at", []model.Value{list(i32(7)), i32(0)}, "7"},
		{"list.at", []model.Value{list(i32(7)), i32(1)}, "none"},
		{"list.at", []model.Value{list(i32(7)), i32(-1)}, "none"},
		{"map.count", []model.Value{mp(s("a"), i32(1))}, "1"},
		{"map.isEmpty", []model.Value{mp()}, "true"},
		{"map.keys", []model.Value{mp(s("b"), i32(1), s("a"), i32(2))}, "[b, a]"},
		{"map.values", []model.Value{mp(s("b"), i32(1), s("a"), i32(2))}, "[1, 2]"},
		{"map.get", []model.Value{mp(s("a"), i32(1)), s("a")}, "1"},
		{"map.get", []model.Value{mp(s("a"), i32(1)), s("b")}, "none"},
		{"map.put", []model.Value{mp(s("a"), i32(1), s("b"), i32(2)), s("a"), i32(9)}, "[a: 9, b: 2]"},
		{"map.put", []model.Value{mp(s("a"), i32(1)), s("c"), i32(3)}, "[a: 1, c: 3]"},
		{"map.contains", []model.Value{mp(s("a"), i32(1)), s("a")}, "true"},
		{"page.items", []model.Value{page}, "[1]"},
		{"page.next", []model.Value{page}, "next"},
		{"pagereq.size", []model.Value{pageReq(a, 20, "")}, "20"},
		{"pagereq.cursor", []model.Value{pageReq(a, 20, "c")}, "c"},
		{"caller.roles", []model.Value{r.callerValue()}, "[admin, support]"},
		{"list.with", []model.Value{list(i32(1)), i32(1)}, "[1, 1]"},
		{"set.with", []model.Value{set(i32(1), i32(2)), i32(1)}, "[1, 2]"},
		{"set.with", []model.Value{set(i32(1)), i32(3)}, "[1, 3]"},
		{"list.without", []model.Value{list(i32(1), i32(2), i32(1)), i32(1)}, "[2]"},
		{"set.without", []model.Value{set(i32(1), i32(2)), i32(5)}, "[1, 2]"},
	}
	for _, coll := range []string{"list", "set"} {
		mk := map[string]func(...model.Value) model.Value{"list": list, "set": set}[coll]
		cases = append(cases,
			tc{coll + ".count", []model.Value{mk(i32(1), i32(2))}, "2"},
			tc{coll + ".isEmpty", []model.Value{mk()}, "true"},
			tc{coll + ".first", []model.Value{mk(i32(1), i32(2))}, "1"},
			tc{coll + ".first", []model.Value{mk()}, "none"},
			tc{coll + ".last", []model.Value{mk(i32(1), i32(2))}, "2"},
			tc{coll + ".contains", []model.Value{mk(i32(1), i32(2)), i32(2)}, "true"},
			tc{coll + ".sum.i32", []model.Value{mk(i32(1), i32(2))}, "3"},
			tc{coll + ".sum.i32", []model.Value{mk(i32(math.MaxInt32), i32(1))}, "!overflow"},
			tc{coll + ".sum.i64", []model.Value{mk(i64(1), i64(2))}, "3"},
			tc{coll + ".sum.f64", []model.Value{mk(fl(0.5), fl(2))}, "2.5"},
			tc{coll + ".sum.dec", []model.Value{mk(decimal("0.1"), decimal("0.2"))}, "0.3"},
		)
	}
	num := map[string]model.Value{"i32": i32(42), "i64": i64(42), "f64": fl(42.9), "dec": decimal("42.9"), "str": s("42")}
	for _, to := range []string{"int", "bigint", "float", "decimal"} {
		for k, v := range num {
			want := "42"
			if to == "float" || to == "decimal" {
				want = map[string]string{"f64": "42.9", "dec": "42.9"}[k]
				if want == "" {
					want = "42"
				}
			}
			cases = append(cases, tc{to + "." + k, []model.Value{v}, want})
		}
	}
	cases = append(cases,
		tc{"int.i64", []model.Value{i64(1 << 40)}, "!overflow: 1099511627776 out of int range"},
		tc{"int.str", []model.Value{s("x")}, `!cannot convert the value: the string is not a number`},
		tc{"bigint.f64", []model.Value{fl(1e30)}, "!overflow: 1e+30"},
		tc{"decimal.f64", []model.Value{fl(1e30)}, "!overflow: 1e+30"},
		tc{"float.str", []model.Value{s("inf")}, `!cannot convert the value: the string is not a number`},
		tc{"decimal.str", []model.Value{s("1e3")}, `!cannot convert the value: the string is not a number`},
	)
	neg := map[string]model.Value{"i32": i32(-5), "i64": i64(-5), "f64": fl(-2.5), "dec": decimal("-2.5")}
	pos := map[string]model.Value{"i32": i32(7), "i64": i64(7), "f64": fl(7), "dec": decimal("7")}
	for _, k := range []string{"i32", "i64", "f64", "dec"} {
		absWant, floorWant, ceilWant := "5", "-5", "-5"
		if k == "f64" || k == "dec" {
			absWant, floorWant, ceilWant = "2.5", "-3", "-2"
		}
		cases = append(cases,
			tc{"abs." + k, []model.Value{neg[k]}, absWant},
			tc{"floor." + k, []model.Value{neg[k]}, floorWant},
			tc{"ceil." + k, []model.Value{neg[k]}, ceilWant},
			tc{"min." + k, []model.Value{pos[k], neg[k]}, r.textOf(neg[k])},
			tc{"max." + k, []model.Value{pos[k], neg[k]}, "7"},
		)
	}
	cases = append(cases,
		tc{"abs.i32", []model.Value{i32(math.MinInt32)}, "!overflow"},
		tc{"round.i32", []model.Value{i32(1250), i32(-2)}, "1300"},
		tc{"round.i32", []model.Value{i32(-1250), i32(-2)}, "-1300"},
		tc{"round.i32", []model.Value{i32(1249), i32(-2)}, "1200"},
		tc{"round.i32", []model.Value{i32(7), i32(2)}, "7"},
		tc{"round.i32", []model.Value{i32(math.MaxInt32), i32(-1)}, "!overflow"},
		tc{"round.i64", []model.Value{i64(5e18), i32(-19)}, "!overflow"},
		tc{"round.i64", []model.Value{i64(4e18), i32(-19)}, "0"},
		tc{"round.i64", []model.Value{i64(5), i32(-30)}, "0"},
		tc{"round.f64", []model.Value{fl(2.345), i32(1)}, "2.3"},
		tc{"round.dec", []model.Value{decimal("2.345"), i32(2)}, "2.35"},
		tc{"round.dec", []model.Value{decimal("-2.5"), i32(0)}, "-3"},
	)
	tested := map[string]bool{}
	for _, c := range cases {
		tested[c.name] = true
		id := bytecode.IntrinsicIndex(c.name)
		if id == bytecode.NoFunc {
			t.Fatalf("no built-in function %s", c.name)
		}
		v, err := builtins[id](r, c.args)
		got := ""
		if err != nil {
			got = "!" + err.Error()
		} else {
			got = r.textOf(v)
		}
		if got != c.want {
			args := make([]string, len(c.args))
			for i, x := range c.args {
				args[i] = r.textOf(x)
			}
			t.Errorf("%s(%s) = %s, expected %s", c.name, strings.Join(args, ", "), got, c.want)
		}
	}
	for _, name := range bytecode.Intrinsics {
		if name == "range" || name == "perf.stat" || strings.HasPrefix(name, "faker.") {
			continue // TestScenarioIntrinsics
		}
		if !tested[name] {
			t.Errorf("built-in function %s is not checked", name)
		}
	}
	if _, err := builtins[bytecode.IntrinsicIndex("str.matches")](r, []model.Value{s("a"), s("(")}); !errors.Is(err, ErrInvalidRegex) {
		t.Errorf("cache of an invalid expression: %v", err)
	}
}

// TestScenarioIntrinsics: range and faker give values of the right kind,
// deterministic by seed and test name, phone and email do not repeat;
// faker outside a test is an error.
func TestScenarioIntrinsics(t *testing.T) {
	_, r := opRun(t)
	call := func(name string, args ...model.Value) (string, error) {
		v, err := builtins[bytecode.IntrinsicIndex(name)](r, args)
		if err != nil {
			return "", err
		}
		return r.textOf(v), nil
	}
	if got, _ := call("range", model.Int(3)); got != "[0, 1, 2]" {
		t.Errorf("range(3) = %s", got)
	}
	if _, err := call("faker.name"); !errors.Is(err, errNoTest) {
		t.Errorf("faker outside a test: %v", err)
	}
	run := func(seed uint64) []string {
		r.test = &testRun{faker: newFaker("test", TestOptions{Seed: seed})}
		defer func() { r.test = nil }()
		var out []string
		for _, name := range bytecode.FakerFuncs {
			var args []model.Value
			switch name {
			case "text":
				args = []model.Value{model.Int(3)}
			case "int":
				args = []model.Value{model.Int(1), model.Int(6)}
			case "decimal", "chance":
				d, _ := dec.Parse("0.5")
				args = []model.Value{model.Decimal(d)}
				if name == "decimal" {
					hi, _ := dec.Parse("10")
					args = append(args, model.Decimal(hi))
				}
			case "float":
				args = []model.Value{model.Float(0), model.Float(1)}
			case "pick":
				args = []model.Value{r.coll(model.KindList, r.a.String("a"), r.a.String("b"))}
			case "date":
				args = []model.Value{model.Date(2026, 1, 1), model.Date(2026, 1, 31)}
			case "pattern":
				args = []model.Value{r.a.String("?###??77")}
			}
			got, err := call("faker."+name, args...)
			if err != nil {
				t.Fatalf("faker.%s: %v", name, err)
			}
			out = append(out, got)
		}
		return out
	}
	a, b, c := run(1), run(1), run(2)
	if strings.Join(a, "|") != strings.Join(b, "|") {
		t.Errorf("one seed, different values:\n%v\n%v", a, b)
	}
	if strings.Join(a, "|") == strings.Join(c, "|") {
		t.Errorf("different seeds, same values: %v", a)
	}
	if n, err := strconv.Atoi(a[8]); err != nil || n < 1 || n > 6 {
		t.Errorf("faker.int(1, 6) = %s", a[8])
	}
	if len([]rune(a[14])) != 8 || !strings.HasSuffix(a[14], "77") {
		t.Errorf("faker.pattern = %s", a[14])
	}
	r.test = &testRun{faker: newFaker("test", TestOptions{})}
	seen := map[string]bool{}
	for range 500 {
		p, _ := call("faker.phone")
		if seen[p] {
			t.Fatalf("phone repeated: %s", p)
		}
		seen[p] = true
	}
	r.test = nil
}
