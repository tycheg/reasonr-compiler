// Package codegen builds bytecode (runtime/bytecode) from a model's IR
// using its schema (RFC §29): type, method
// and field names become schema numbers, slots become registers, blocks
// become code with jumps, operations with a variable number of operands
// become call table entries, and numeric operations and built-in functions
// get variants by operand kind.
package codegen

import (
	"fmt"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ir"
	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
)

// Generate builds the program. An error means the IR and the schema
// disagree, which is a compiler failure.
func Generate(p *ir.Program, s *model.Schema) (*bytecode.Program, error) {
	g := &gen{
		ir:     p,
		schema: s,
		out:    &bytecode.Program{Schema: s, Files: append([]string(nil), p.Files...)},
		types:  map[string]model.TypeRef{},
		consts: map[string]uint32{},
		funcs:  map[string]int32{},
	}
	for _, t := range s.Types {
		g.types[t.Name] = t.Ref
	}
	for i, f := range p.Funcs {
		g.funcs[funcKey(f.Kind, f.Name)] = int32(i)
	}
	for _, f := range p.Funcs {
		bf, err := g.function(f)
		if err != nil {
			return nil, fmt.Errorf("codegen: %s %s: %w", f.Kind, f.Name, err)
		}
		g.out.Funcs = append(g.out.Funcs, bf)
	}
	for _, r := range p.Reactions {
		t, m, err := g.method(r.Event)
		if err != nil {
			return nil, fmt.Errorf("codegen: reaction to %s: %w", r.Event, err)
		}
		br := bytecode.Reaction{Type: t, Event: m}
		for _, name := range r.Funcs {
			fi, ok := g.funcs[funcKey(ir.FuncReaction, name)]
			if !ok {
				fi, ok = g.funcs[funcKey(ir.FuncACL, name)]
			}
			if !ok {
				return nil, fmt.Errorf("codegen: reaction to %s: no function %s", r.Event, name)
			}
			br.Funcs = append(br.Funcs, fi)
		}
		g.out.Reactions = append(g.out.Reactions, br)
	}
	return g.out, nil
}

func funcKey(k ir.FuncKind, name string) string { return k.String() + " " + name }

type gen struct {
	ir     *ir.Program
	schema *model.Schema
	out    *bytecode.Program
	types  map[string]model.TypeRef
	consts map[string]uint32 // constant key → number in the pool
	funcs  map[string]int32
}

// typeRef returns the number of a type by name.
func (g *gen) typeRef(name string) (model.TypeRef, error) {
	if t, ok := g.types[name]; ok {
		return t, nil
	}
	return 0, fmt.Errorf("no type %s in the schema", name)
}

// method parses "Type.method" and returns the type and method numbers.
func (g *gen) method(sym string) (model.TypeRef, model.MethodRef, error) {
	// The type name is full and dotted (Ordering.Order): the method comes after the last dot.
	i := strings.LastIndex(sym, ".")
	tn, mn, ok := sym[:max(i, 0)], sym[i+1:], i > 0
	if !ok {
		return 0, 0, fmt.Errorf("invalid method name %q", sym)
	}
	t, err := g.typeRef(tn)
	if err != nil {
		return 0, 0, err
	}
	m := g.schema.Type(t).Method(mn)
	if m == nil {
		return 0, 0, fmt.Errorf("type %s has no method %s", tn, mn)
	}
	return t, m.Ref, nil
}

// feature returns the number of a feature flag in the schema, or -1.
func (g *gen) feature(name string) int {
	for i := range g.schema.Features {
		if g.schema.Features[i].Name == name {
			return i
		}
	}
	return -1
}

// field returns the number of a field of type t.
func (g *gen) field(t model.TypeRef, name string) (model.FieldRef, error) {
	ti := g.schema.Type(t)
	if ti == nil {
		return 0, fmt.Errorf("no type %d", t)
	}
	if f := ti.Field(name); f != nil {
		return f.Ref, nil
	}
	return 0, fmt.Errorf("type %s has no field %s", ti.Name, name)
}

// param returns the number of a parameter of method m of type t.
func (g *gen) param(t model.TypeRef, m model.MethodRef, name string) (model.FieldRef, error) {
	mi := &g.schema.Type(t).Methods[m]
	for _, p := range mi.Params {
		if p.Name == name {
			return p.Ref, nil
		}
	}
	return 0, fmt.Errorf("method %s has no parameter %s", mi.Name, name)
}

// typ converts an IR type into a schema type. A reference [T] is the
// identifier of T; caller is a record without a type.
func (g *gen) typ(t *ir.Type) (model.Type, error) {
	if t == nil {
		return model.Type{}, fmt.Errorf("no type")
	}
	kinds := map[ir.TypeKind]model.Kind{
		ir.TInt: model.KindInt, ir.TBigint: model.KindBigint, ir.TFloat: model.KindFloat,
		ir.TDecimal: model.KindDecimal, ir.TString: model.KindString, ir.TBool: model.KindBool,
		ir.TUUID: model.KindUUID, ir.TDate: model.KindDate, ir.TTime: model.KindTime,
		ir.TTimestamp: model.KindTimestamp, ir.TDuration: model.KindDuration, ir.TBytes: model.KindBytes,
		ir.TTimer: model.KindTimerID, ir.TCaller: model.KindRecord, ir.TPageReq: model.KindPageRequest,
		ir.TEnum: model.KindEnum, ir.TValue: model.KindRecord, ir.TRecord: model.KindRecord,
		ir.TID: model.KindID, ir.TRef: model.KindID,
		ir.TList: model.KindList, ir.TSet: model.KindSet, ir.TMap: model.KindMap, ir.TPage: model.KindPage,
	}
	if t.Kind == ir.TOptional {
		out, err := g.typ(t.Elem)
		out.Optional = true
		return out, err
	}
	k, ok := kinds[t.Kind]
	if !ok {
		return model.Type{}, fmt.Errorf("unknown type kind %s", t.Kind)
	}
	out := model.Type{Kind: k}
	if t.Name != "" {
		ref, err := g.typeRef(t.Name)
		if err != nil {
			return out, err
		}
		out.Ref = ref
	}
	if t.Elem != nil {
		e, err := g.typ(t.Elem)
		if err != nil {
			return out, err
		}
		out.Elem = &e
	}
	if t.Key != nil {
		k, err := g.typ(t.Key)
		if err != nil {
			return out, err
		}
		out.Key = &k
	}
	return out, nil
}

// konst returns the number of a constant in the program pool.
func (g *gen) konst(c ir.Const) (uint32, error) {
	key := c.Key()
	if i, ok := g.consts[key]; ok {
		return i, nil
	}
	t, err := g.typ(c.Type.Unwrap())
	if err != nil {
		return 0, err
	}
	bc := bytecode.Const{Type: t, Lit: model.Literal{Kind: t.Kind, Text: c.Text}}
	if c.None {
		bc.Lit = model.Literal{Kind: model.KindNone}
	}
	i := uint32(len(g.out.Consts))
	g.out.Consts = append(g.out.Consts, bc)
	g.consts[key] = i
	return i, nil
}

// function builds a function.
func (g *gen) function(f *ir.Func) (bytecode.Func, error) {
	out := bytecode.Func{Name: f.Name, Kind: bytecode.FuncKind(f.Kind), Method: bytecode.NoMethod, Params: f.Params}
	if f.Owner != "" {
		t, err := g.typeRef(f.Owner)
		if err != nil {
			return out, err
		}
		out.Owner = t
		if m, ok := g.ownMethod(f); ok {
			ti := g.schema.Type(t)
			mi := ti.Method(m)
			switch {
			case mi != nil:
				out.Method = mi.Ref
			default:
				return out, fmt.Errorf("type %s has no method %s", ti.Name, m)
			}
		}
	}
	for _, s := range f.Slots {
		t, err := g.typ(s.Type)
		if err != nil {
			return out, err
		}
		out.Regs = append(out.Regs, t)
		out.RegNames = append(out.RegNames, s.Name)
	}
	for _, c := range f.Consts {
		i, err := g.konst(c)
		if err != nil {
			return out, err
		}
		out.Consts = append(out.Consts, i)
	}
	if f.Result != nil {
		t, err := g.typ(f.Result)
		if err != nil {
			return out, err
		}
		out.Result = &t
	}
	fg := &fgen{g: g, f: f, out: &out}
	if err := fg.code(); err != nil {
		return out, err
	}
	return out, nil
}

// ownMethod returns the name of the schema method the function implements:
// for commands, applications, queries, rules and methods the name after
// "Type.", for a reaction its event.
func (g *gen) ownMethod(f *ir.Func) (string, bool) {
	rest, ok := strings.CutPrefix(f.Name, f.Owner+".")
	if !ok {
		return "", false
	}
	switch f.Kind {
	case ir.FuncCommand, ir.FuncApply, ir.FuncQuery, ir.FuncRule, ir.FuncMethod:
		return rest, true
	case ir.FuncReaction, ir.FuncACL:
		// "on.placed", "on.placed#2", "on.placed@Dispatch" — event placed.
		ev := strings.TrimPrefix(rest, "on.")
		if i := strings.IndexAny(ev, "#@"); i >= 0 {
			ev = ev[:i]
		}
		return ev, true
	}
	return "", false
}
