package bytecode

import (
	"fmt"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/model"
)

// Verify checks a program before it runs: instructions decode; registers,
// fields, types, methods, functions and table entries exist; jumps lead to
// instruction starts; code does not run past the end of a function; and
// the operands of typed operations, conditions and returns are of the right
// kind. The VM runs only verified programs and does not recheck this.
func Verify(p *Program) error {
	if p.Schema == nil {
		return fmt.Errorf("bytecode: no schema")
	}
	if _, _, err := p.Constants(); err != nil {
		return err
	}
	for i, c := range p.Consts {
		if err := verifyType(p.Schema, &c.Type, 0); err != nil {
			return fmt.Errorf("bytecode: constant %d: %w", i, err)
		}
	}
	for i := range p.Funcs {
		v := &verifier{p: p, f: &p.Funcs[i]}
		if err := v.fn(); err != nil {
			return fmt.Errorf("bytecode: %s %s: %w", p.Funcs[i].Kind, p.Funcs[i].Name, err)
		}
	}
	for _, r := range p.Reactions {
		if _, err := methodOf(p.Schema, r.Type, r.Event, model.MethodEvent); err != nil {
			return fmt.Errorf("bytecode: reaction: %w", err)
		}
		for _, fi := range r.Funcs {
			if fi < 0 || int(fi) >= len(p.Funcs) || p.Funcs[fi].Kind != FuncReaction && p.Funcs[fi].Kind != FuncACL {
				return fmt.Errorf("bytecode: reaction to %d.%d: function %d is not a reaction", r.Type, r.Event, fi)
			}
		}
	}
	return nil
}

func verifyType(s *model.Schema, t *model.Type, depth int) error {
	if depth > maxDepth {
		return fmt.Errorf("type too deep")
	}
	if t.Kind > model.KindPageRequest {
		return fmt.Errorf("unknown kind %d", t.Kind)
	}
	switch t.Kind {
	case model.KindEnum, model.KindID:
		if s.Type(t.Ref) == nil {
			return fmt.Errorf("no type %d", t.Ref)
		}
	case model.KindRecord:
		if t.Ref != 0 && s.Type(t.Ref) == nil {
			return fmt.Errorf("no type %d", t.Ref)
		}
	}
	needElem := t.Kind == model.KindList || t.Kind == model.KindSet || t.Kind == model.KindMap || t.Kind == model.KindPage
	if needElem != (t.Elem != nil) || (t.Kind == model.KindMap) != (t.Key != nil) {
		return fmt.Errorf("type %s: invalid parameters", t.Kind)
	}
	if t.Elem != nil {
		if err := verifyType(s, t.Elem, depth+1); err != nil {
			return err
		}
	}
	if t.Key != nil {
		return verifyType(s, t.Key, depth+1)
	}
	return nil
}

// methodOf returns method m of type t, checking its kind.
func methodOf(s *model.Schema, t model.TypeRef, m model.MethodRef, kind model.MethodKind) (*model.MethodInfo, error) {
	ti := s.Type(t)
	if ti == nil {
		return nil, fmt.Errorf("no type %d", t)
	}
	if int(m) >= len(ti.Methods) {
		return nil, fmt.Errorf("type %s has no method %d", ti.Name, m)
	}
	mi := &ti.Methods[m]
	if mi.Kind != kind {
		return nil, fmt.Errorf("%s.%s is %s, expected %s", ti.Name, mi.Name, mi.Kind, kind)
	}
	return mi, nil
}

type verifier struct {
	p *Program
	f *Func
}

func (v *verifier) fn() error {
	f, s := v.f, v.p.Schema
	if f.Kind > FuncACL {
		return fmt.Errorf("unknown function kind %d", f.Kind)
	}
	if f.Owner != 0 && s.Type(f.Owner) == nil {
		return fmt.Errorf("no type %d", f.Owner)
	}
	if f.Method != NoMethod && (f.Owner == 0 || int(f.Method) >= len(s.Type(f.Owner).Methods)) {
		return fmt.Errorf("no method %d", f.Method)
	}
	if f.Params < 0 || f.Params > len(f.Regs) || len(f.RegNames) != len(f.Regs) {
		return fmt.Errorf("%d parameters, %d registers, %d names", f.Params, len(f.Regs), len(f.RegNames))
	}
	if f.Frame() > 0xffff {
		return fmt.Errorf("frame %d exceeds 65535", f.Frame())
	}
	for i := range f.Regs {
		if err := verifyType(s, &f.Regs[i], 0); err != nil {
			return fmt.Errorf("r%d: %w", i, err)
		}
	}
	for _, c := range f.Consts {
		if int(c) >= len(v.p.Consts) {
			return fmt.Errorf("no constant k%d", c)
		}
	}
	if f.Result != nil {
		if err := verifyType(s, f.Result, 0); err != nil {
			return fmt.Errorf("result: %w", err)
		}
	}
	if len(f.Code) == 0 {
		return fmt.Errorf("no code")
	}
	starts := map[uint32]bool{}
	var targets []uint32
	var last Instr
	for pc := 0; pc < len(f.Code); {
		in, ok := DecodeInstr(f.Code, pc)
		if !ok {
			return fmt.Errorf("%d: invalid instruction %#08x", pc, f.Code[pc])
		}
		starts[uint32(pc)] = true
		if err := v.instr(in); err != nil {
			return fmt.Errorf("%d: %s: %w", pc, in.Op, err)
		}
		if in.Op.Info().Roles[1] == RoleTarget {
			targets = append(targets, in.B)
		}
		last = in
		pc += in.Size
	}
	if !last.Op.Info().Term {
		return fmt.Errorf("the code ends with neither a jump nor a return (%s)", last.Op)
	}
	for _, t := range targets {
		if !starts[t] {
			return fmt.Errorf("jump to %d is not the start of an instruction", t)
		}
	}
	if err := v.assigned(); err != nil {
		return err
	}
	for i, l := range f.Lines {
		switch {
		case !starts[l.PC]:
			return fmt.Errorf("position %d: address %d is not the start of an instruction", i, l.PC)
		case i > 0 && l.PC <= f.Lines[i-1].PC:
			return fmt.Errorf("position %d: addresses do not increase", i)
		case l.File == 0 || int(l.File) > len(v.p.Files) || l.Line == 0:
			return fmt.Errorf("position %d: invalid file %d or line %d", i, l.File, l.Line)
		}
	}
	return nil
}

// regType returns the type of a register: a slot or a constant (optional for none).
func (v *verifier) regType(r uint32) (model.Type, error) {
	f := v.f
	switch {
	case int(r) < len(f.Regs):
		return f.Regs[r], nil
	case int(r) < f.Frame():
		c := v.p.Consts[f.Consts[int(r)-len(f.Regs)]]
		t := c.Type
		if c.Lit.Kind == model.KindNone {
			t.Optional = true
		}
		return t, nil
	}
	return model.Type{}, fmt.Errorf("no register r%d", r)
}

func (v *verifier) fields(t model.TypeRef) ([]model.FieldInfo, error) {
	ti := v.p.Schema.Type(t)
	if ti == nil {
		return nil, fmt.Errorf("no type %d", t)
	}
	return ti.Fields, nil
}

func (v *verifier) instr(in Instr) error {
	info := in.Op.Info()
	vals := [3]uint32{in.A, in.B, in.C}
	for i, role := range info.Roles {
		x := vals[i]
		switch role {
		case RoleNone:
			if x != 0 && in.Op != OpWide {
				return fmt.Errorf("extra operand %d", x)
			}
		case RoleDst:
			if int(x) >= len(v.f.Regs) {
				return fmt.Errorf("the result is not in a slot register: r%d", x)
			}
		case RoleReg:
			if _, err := v.regType(x); err != nil {
				return err
			}
		case RoleField:
			fs, err := v.fields(v.f.Owner)
			if err != nil {
				return err
			}
			if int(x) >= len(fs) {
				return fmt.Errorf("no field %d", x)
			}
		case RoleRegField:
			t, err := v.regType(in.B)
			if err != nil {
				return err
			}
			fs, err := v.fields(t.Ref)
			if err != nil {
				return err
			}
			if int(x) >= len(fs) {
				return fmt.Errorf("no field %d", x)
			}
		case RoleType:
			if v.p.Schema.Type(model.TypeRef(x)) == nil {
				return fmt.Errorf("no type %d", x)
			}
		case RoleFeature:
			if int(x) >= len(v.p.Schema.Features) {
				return fmt.Errorf("no feature flag %d", x)
			}
		case RoleSite:
			if int(x) >= len(v.f.Sites) {
				return fmt.Errorf("no site %d", x)
			}
			if err := v.site(in.Op, &v.f.Sites[x]); err != nil {
				return fmt.Errorf("site %d: %w", x, err)
			}
		case RoleFind:
			if int(x) >= len(v.f.Finds) {
				return fmt.Errorf("no search template %d", x)
			}
			if err := v.find(&v.f.Finds[x]); err != nil {
				return err
			}
		}
	}
	return v.types(in)
}

// siteKinds are the method kind and the called function kind of operations with a site.
var siteKinds = map[Op]struct {
	method model.MethodKind
	fn     FuncKind
	needFn bool
}{
	OpEvent:        {model.MethodEvent, FuncApply, false},
	OpNotify:       {model.MethodNotification, 0, false},
	OpError:        {model.MethodError, 0, false},
	OpCmd:          {model.MethodCommand, FuncCommand, true},
	OpCmdOpt:       {model.MethodCommand, FuncCommand, true},
	OpCallOut:      {model.MethodCommand, FuncCommand, true},
	OpSchedule:     {model.MethodCommand, FuncCommand, true},
	OpRequest:      {model.MethodRequest, 0, false},
	OpQueryCall:    {model.MethodQuery, FuncQuery, true},
	OpRuleCall:     {model.MethodRule, FuncRule, true},
	OpMethod:       {model.MethodValue, FuncMethod, true},
	OpTestCmd:      {model.MethodCommand, FuncCommand, true},
	OpTestQuery:    {model.MethodQuery, FuncQuery, true},
	OpExpectNotify: {model.MethodNotification, 0, false},
	OpExpectError:  {model.MethodError, 0, false},
	OpExpectReq:    {model.MethodRequest, 0, false},
}

// optionalArg reports whether site argument i may be NoReg.
func optionalArg(op Op, i int) bool {
	switch op {
	case OpNotify, OpExpectNotify, OpTestQuery:
		return i == 0
	case OpTestCmd:
		return i == 0 || i == 1
	}
	return false
}

func (v *verifier) site(op Op, s *Site) error {
	for i, a := range s.Args {
		if a == NoReg && optionalArg(op, i) {
			continue
		}
		if _, err := v.regType(a); err != nil {
			return err
		}
	}
	fixed := FixedArgs(op)
	if len(s.Args) < fixed {
		return fmt.Errorf("%d arguments, at least %d needed", len(s.Args), fixed)
	}
	named := len(s.Args) - fixed
	checkFields := func(all int, allowID bool) error {
		if s.Fields == nil {
			if named != all {
				return fmt.Errorf("%d arguments, %d parameters", named, all)
			}
			return nil
		}
		if len(s.Fields) != named {
			return fmt.Errorf("%d arguments, %d names", named, len(s.Fields))
		}
		for _, f := range s.Fields {
			if int(f) >= all && !(allowID && f == IDField) {
				return fmt.Errorf("no field or parameter %d", f)
			}
		}
		return nil
	}
	if k, ok := siteKinds[op]; ok {
		if op == OpExpectError && s.Type == 0 {
			params, ok := BuiltinErrors[s.Name]
			if !ok {
				return fmt.Errorf("no built-in error %q", s.Name)
			}
			return checkFields(len(params), false)
		}
		mi, err := methodOf(v.p.Schema, s.Type, s.Method, k.method)
		if err != nil {
			return err
		}
		if err := v.callee(s.Func, k.fn, k.needFn); err != nil {
			return err
		}
		return checkFields(len(mi.Params), false)
	}
	switch op {
	case OpValNew, OpExpectMatch, OpUpsert:
		ti := v.p.Schema.Type(s.Type)
		want := model.TypeValue
		if op == OpUpsert {
			want = model.TypeProjection
		}
		if ti == nil || ti.Kind != want {
			return fmt.Errorf("type %d is not %v", s.Type, want)
		}
		if op == OpValNew {
			if s.Fields != nil {
				return fmt.Errorf("val.new passes all fields")
			}
			if err := v.callee(s.Func, FuncCheck, false); err != nil {
				return err
			}
		}
		if op == OpUpsert && (len(s.Fields) == 0 || s.Fields[0] != IDField) {
			return fmt.Errorf("upsert without id")
		}
		return checkFields(len(ti.Fields), op == OpUpsert)
	case OpCollMap:
		if len(s.Args)%2 != 0 {
			return fmt.Errorf("an odd number of keys and values")
		}
	case OpIntrinsic:
		if s.Func < 0 || int(s.Func) >= len(Intrinsics) {
			return fmt.Errorf("no built-in function %d", s.Func)
		}
		sig := IntrinsicSigs[s.Func]
		if n := len(s.Args); n != len(sig.Args) && !(sig.Variadic && n >= 1) {
			return fmt.Errorf("%s: %d arguments, %d expected", Intrinsics[s.Func], n, len(sig.Args))
		}
	}
	return nil
}

// callee checks the called function.
func (v *verifier) callee(fi int32, kind FuncKind, need bool) error {
	if fi == NoFunc {
		if need {
			return fmt.Errorf("no called function")
		}
		return nil
	}
	if fi < 0 || int(fi) >= len(v.p.Funcs) || v.p.Funcs[fi].Kind != kind {
		return fmt.Errorf("function %d is not %s", fi, kind)
	}
	return nil
}

func (v *verifier) find(fd *Find) error {
	ti := v.p.Schema.Type(fd.Type)
	if ti == nil || ti.ID == model.IDNone {
		return fmt.Errorf("search on %d: neither an entity nor a projection", fd.Type)
	}
	if fd.Index < -1 || int(fd.Index) >= len(ti.Indexes) {
		return fmt.Errorf("no index %d", fd.Index)
	}
	for _, c := range fd.Where {
		if c.Op > model.OpIn {
			return fmt.Errorf("unknown condition %d", c.Op)
		}
		if err := v.path(fd.Type, c.Field); err != nil {
			return err
		}
		if _, err := v.regType(c.Value); err != nil {
			return err
		}
	}
	for _, k := range fd.Sort {
		if err := v.path(fd.Type, k.Field); err != nil {
			return err
		}
	}
	for _, r := range []uint32{fd.Page, fd.Limit} {
		if r == NoReg {
			continue
		}
		if _, err := v.regType(r); err != nil {
			return err
		}
	}
	return nil
}

func (v *verifier) path(t model.TypeRef, p model.FieldPath) error {
	if len(p) == 0 {
		return fmt.Errorf("empty path")
	}
	owner := t
	for i, f := range p {
		if f == IDField && i == 0 {
			owner = 0
			continue
		}
		ti := v.p.Schema.Type(owner)
		if ti == nil || int(f) >= len(ti.Fields) {
			return fmt.Errorf("path: no field %d", f)
		}
		owner = 0
		if ft := ti.Fields[f].Type; ft.Kind == model.KindRecord {
			owner = ft.Ref
		}
	}
	return nil
}

// kindOf is the value kind by the suffix of a typed operation.
var kindOf = map[string]model.Kind{
	"i32": model.KindInt, "i64": model.KindBigint, "f64": model.KindFloat, "dec": model.KindDecimal,
	"str": model.KindString, "bool": model.KindBool, "uuid": model.KindUUID, "date": model.KindDate,
	"time": model.KindTime, "ts": model.KindTimestamp, "dur": model.KindDuration, "bytes": model.KindBytes,
	"enum": model.KindEnum, "id": model.KindID, "timer": model.KindTimerID, "rec": model.KindRecord,
	"list": model.KindList, "set": model.KindSet, "map": model.KindMap,
}

// types checks the operand kinds of typed operations, conditions and value
// returns.
func (v *verifier) types(in Instr) error {
	want := func(r uint32, k model.Kind, optional bool) error {
		t, err := v.regType(r)
		if err != nil {
			return err
		}
		if t.Kind != k || t.Optional && !optional {
			return fmt.Errorf("r%d: %s, expected %s", r, typeText(t), k)
		}
		return nil
	}
	switch in.Op {
	case OpBrTrue, OpBrFalse:
		return want(in.A, model.KindBool, false)
	case OpNot:
		if err := want(in.B, model.KindBool, false); err != nil {
			return err
		}
		return want(in.A, model.KindBool, false)
	case OpIsNone, OpHasErrors, OpFeature, OpRefExists, OpInList, OpInSet, OpInMap:
		return want(in.A, model.KindBool, true)
	case OpRetVal:
		if v.f.Result == nil {
			return fmt.Errorf("the function has no result")
		}
		return want(in.A, v.f.Result.Kind, true)
	case OpMove:
		src, _ := v.regType(in.B)
		return want(in.A, src.Kind, true)
	}
	if int(in.Op) < NumFixedOps() {
		return nil
	}
	base, suffix, _ := strings.Cut(in.Op.String(), ".")
	var a, b, dst model.Kind
	unary := base == "neg"
	switch in.Op.String() {
	case "add.str":
		a, b, dst = model.KindString, model.KindString, model.KindString
	case "add.dur", "sub.dur", "neg.dur":
		a, b, dst = model.KindDuration, model.KindDuration, model.KindDuration
	case "mul.dur": // duration × int or bigint
		a, b, dst = model.KindDuration, model.KindBigint, model.KindDuration
		if t, err := v.regType(in.C); err == nil && t.Kind == model.KindInt {
			b = model.KindInt
		}
	case "add.ts", "sub.ts":
		a, b, dst = model.KindTimestamp, model.KindDuration, model.KindTimestamp
	case "diff.ts":
		a, b, dst = model.KindTimestamp, model.KindTimestamp, model.KindDuration
	default:
		k := kindOf[suffix]
		a, b, dst = k, k, k
		if base == "eq" || base == "ne" || base == "lt" || base == "le" || base == "gt" || base == "ge" {
			dst = model.KindBool
		}
	}
	optional := base == "eq" || base == "ne" // comparison with a missing value
	if err := want(in.B, a, optional); err != nil {
		return err
	}
	if !unary {
		if err := want(in.C, b, optional); err != nil {
			return err
		}
	}
	return want(in.A, dst, false)
}

func typeText(t model.Type) string {
	s := t.Kind.String()
	if t.Optional {
		s += "?"
	}
	return s
}
