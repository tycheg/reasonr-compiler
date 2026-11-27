package schemagen

import (
	"math/big"
	"strconv"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/constant"
	"github.com/tycheg/reasonr-compiler/internal/ir"
	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// Zero values of types (RFC §7.1) in canonical form.
var zeroText = map[types.BasicKind]string{
	types.UUID:      "00000000-0000-0000-0000-000000000000",
	types.Date:      "0001-01-01",
	types.Time:      "00:00",
	types.Timestamp: "0001-01-01T00:00:00Z",
	types.Duration:  "0",
}

// literal converts a default value constant into a schema literal.
func (g *gen) literal(v constant.Value, t types.Type) *model.Literal {
	if _, none := v.(constant.None); none {
		return &model.Literal{Kind: model.KindNone}
	}
	t = types.Unwrap(t)
	switch t := t.(type) {
	case *types.Basic:
		k := basicKinds[t.Kind]
		switch v := v.(type) {
		case bool:
			return &model.Literal{Kind: k, Text: strconv.FormatBool(v)}
		case string:
			return &model.Literal{Kind: k, Text: v}
		case *big.Rat:
			return &model.Literal{Kind: k, Text: number(v, t.Kind)}
		case constant.Text:
			return &model.Literal{Kind: k, Text: text(string(v), t.Kind)}
		}
	case *types.Enum:
		if e, ok := v.(constant.Enum); ok {
			return &model.Literal{Kind: model.KindEnum, Text: e.Name}
		}
	case *types.Value:
		if s, ok := v.(constant.Struct); ok {
			out := &model.Literal{Kind: model.KindRecord}
			for _, f := range t.Decl.Fields {
				if fv, ok := s.Fields[f.Name]; ok {
					out.Fields = append(out.Fields, *g.literal(fv, f.Type))
				}
			}
			return out
		}
	case *types.List, *types.Set, *types.Map:
		if l, ok := v.(constant.List); ok {
			return g.collection(l, t)
		}
	}
	return nil
}

func (g *gen) collection(l constant.List, t types.Type) *model.Literal {
	var out *model.Literal
	var elem types.Type
	var key types.Type
	switch t := t.(type) {
	case *types.List:
		out, elem = &model.Literal{Kind: model.KindList}, t.Elem
	case *types.Set:
		out, elem = &model.Literal{Kind: model.KindSet}, t.Elem
	case *types.Map:
		out, elem, key = &model.Literal{Kind: model.KindMap}, t.Elem, t.Key
	}
	for i, v := range l {
		et := elem
		if key != nil && i%2 == 0 {
			et = key
		}
		if lit := g.literal(v, et); lit != nil {
			out.Elems = append(out.Elems, *lit)
		}
	}
	return out
}

// number is the canonical form of a number by type.
func number(r *big.Rat, k types.BasicKind) string {
	switch k {
	case types.Float:
		f, _ := r.Float64()
		return strconv.FormatFloat(f, 'g', -1, 64)
	case types.Int, types.Bigint:
		return r.Num().String()
	}
	s, ok := ir.RatText(r)
	if !ok {
		return r.FloatString(38)
	}
	return s
}

// text is a duration in nanoseconds, a date or a time without quotes; zero
// values ("zero timestamp") are the canonical zero of the type.
func text(raw string, k types.BasicKind) string {
	if strings.HasPrefix(raw, "zero ") {
		return zeroText[k]
	}
	if k == types.Duration {
		if ns, ok := ir.ParseDuration(raw); ok {
			return strconv.FormatInt(ns, 10)
		}
	}
	if i := strings.IndexByte(raw, '"'); i >= 0 {
		return strings.Trim(raw[i:], `"`)
	}
	return raw
}
