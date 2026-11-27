package bytecode

import (
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/model"
)

// Intrinsics are the built-in functions of the standard library (RFC §8.3)
// in number order (Site.Func of intrinsic). Numeric functions differ by
// argument kind — i32 (int), i64 (bigint), f64, dec, str — so the VM does
// not check it at run time; sum differs by element kind. A function number
// is part of the format: new ones go at the end.
var Intrinsics = buildIntrinsics()

var intrinsicIndex = map[string]int32{}

func buildIntrinsics() []string {
	num := []string{"i32", "i64", "f64", "dec"}
	var out []string
	for _, to := range []string{"int", "bigint", "float", "decimal"} {
		for _, from := range append(num, "str") {
			out = append(out, to+"."+from)
		}
	}
	out = append(out, "string")
	for _, fn := range []string{"abs", "floor", "ceil", "min", "max", "round"} {
		for _, k := range num {
			out = append(out, fn+"."+k)
		}
	}
	out = append(out,
		"str.length", "str.trim", "str.lower", "str.upper", "str.startsWith", "str.contains", "str.matches", "str.concat",
		"ts.date", "dur.seconds", "date.addDays", "ts.addDays", "date.between", "ts.between",
	)
	for _, coll := range []string{"list", "set"} {
		for _, m := range []string{"count", "isEmpty", "first", "last", "contains", "with", "without"} {
			out = append(out, coll+"."+m)
		}
		for _, k := range num {
			out = append(out, coll+".sum."+k)
		}
	}
	out = append(out, "list.at",
		"map.count", "map.isEmpty", "map.keys", "map.values", "map.get", "map.put", "map.contains",
		"page.items", "page.next", "pagereq.size", "pagereq.cursor", "caller.roles",
	)
	// Tests and demo scenarios (RFC §27.5): range and faker.
	out = append(out, "range", "perf.stat")
	for _, f := range FakerFuncs {
		out = append(out, "faker."+f)
	}
	for i, name := range out {
		intrinsicIndex[name] = int32(i)
	}
	return out
}

// FakerFuncs are the faker functions: the first seven take no arguments.
var FakerFuncs = []string{"firstName", "lastName", "name", "phone", "email", "address", "company",
	"text", "int", "decimal", "float", "chance", "pick", "date", "pattern"}

// Sig is the signature of a built-in function: argument kinds (KindNone is
// any); Variadic: any number of arguments of kind Args[0], at least one.
type Sig struct {
	Args     []model.Kind
	Variadic bool
}

// IntrinsicSigs are the signatures of built-in functions by number: the
// verifier checks the number of arguments, the VM the value kinds.
var IntrinsicSigs = buildSigs()

func buildSigs() []Sig {
	kinds := map[string]model.Kind{
		"i32": model.KindInt, "i64": model.KindBigint, "f64": model.KindFloat, "dec": model.KindDecimal,
		"str": model.KindString, "int": model.KindInt, "bigint": model.KindBigint, "float": model.KindFloat,
		"decimal": model.KindDecimal, "list": model.KindList, "set": model.KindSet, "map": model.KindMap,
	}
	const any = model.KindNone
	ts, date := model.KindTimestamp, model.KindDate
	fixed := map[string]Sig{
		"string":     {Args: []model.Kind{any}},
		"str.length": {Args: []model.Kind{model.KindString}}, "str.trim": {Args: []model.Kind{model.KindString}},
		"str.lower": {Args: []model.Kind{model.KindString}}, "str.upper": {Args: []model.Kind{model.KindString}},
		"str.startsWith": {Args: []model.Kind{model.KindString, model.KindString}},
		"str.contains":   {Args: []model.Kind{model.KindString, model.KindString}},
		"str.matches":    {Args: []model.Kind{model.KindString, model.KindString}},
		"str.concat":     {Args: []model.Kind{model.KindString}, Variadic: true},
		"ts.date":        {Args: []model.Kind{ts}}, "dur.seconds": {Args: []model.Kind{model.KindDuration}},
		"date.addDays": {Args: []model.Kind{date, any}}, "ts.addDays": {Args: []model.Kind{ts, any}},
		"date.between": {Args: []model.Kind{date, date, date}}, "ts.between": {Args: []model.Kind{ts, ts, ts}},
		"list.at":      {Args: []model.Kind{model.KindList, any}},
		"map.get":      {Args: []model.Kind{model.KindMap, any}},
		"map.put":      {Args: []model.Kind{model.KindMap, any, any}},
		"map.contains": {Args: []model.Kind{model.KindMap, any}},
		"page.items":   {Args: []model.Kind{model.KindPage}}, "page.next": {Args: []model.Kind{model.KindPage}},
		"pagereq.size":   {Args: []model.Kind{model.KindPageRequest}},
		"pagereq.cursor": {Args: []model.Kind{model.KindPageRequest}},
		"caller.roles":   {Args: []model.Kind{model.KindRecord}},
		"range":          {Args: []model.Kind{model.KindInt}},
		"perf.stat":      {Args: []model.Kind{model.KindString}},
		"faker.text":     {Args: []model.Kind{model.KindInt}},
		"faker.int":      {Args: []model.Kind{model.KindInt, model.KindInt}},
		"faker.decimal":  {Args: []model.Kind{model.KindDecimal, model.KindDecimal}},
		"faker.float":    {Args: []model.Kind{model.KindFloat, model.KindFloat}},
		"faker.chance":   {Args: []model.Kind{model.KindDecimal}},
		"faker.date":     {Args: []model.Kind{date, date}},
		"faker.pattern":  {Args: []model.Kind{model.KindString}},
		"faker.pick":     {Args: []model.Kind{model.KindList}},
	}
	for _, f := range FakerFuncs[:7] { // no arguments
		fixed["faker."+f] = Sig{Args: []model.Kind{}}
	}
	out := make([]Sig, len(Intrinsics))
	for i, name := range Intrinsics {
		if sig, ok := fixed[name]; ok {
			out[i] = sig
			continue
		}
		head, tail, _ := strings.Cut(name, ".")
		last := tail[strings.LastIndex(tail, ".")+1:]
		switch {
		case head == "min" || head == "max":
			out[i] = Sig{Args: []model.Kind{kinds[last], kinds[last]}}
		case head == "round":
			out[i] = Sig{Args: []model.Kind{kinds[last], any}}
		case head == "abs" || head == "floor" || head == "ceil":
			out[i] = Sig{Args: []model.Kind{kinds[last]}}
		case head == "list" || head == "set" || head == "map":
			args := []model.Kind{kinds[head]}
			if tail == "contains" || tail == "with" || tail == "without" {
				args = append(args, any)
			}
			out[i] = Sig{Args: args}
		default: // conversions kind.from
			out[i] = Sig{Args: []model.Kind{kinds[last]}}
		}
	}
	return out
}

// IntrinsicIndex returns the number of a built-in function or NoFunc.
func IntrinsicIndex(name string) int32 {
	if i, ok := intrinsicIndex[name]; ok {
		return i
	}
	return NoFunc
}
