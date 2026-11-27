package bytecode

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/model"
)

// BuiltinErrors are the built-in errors and their parameters (RFC §14): for
// expect.error with Site.Type == 0 the numbers in Site.Fields index this list.
var BuiltinErrors = map[string][]string{"forbidden": nil, "invalidArgument": {"path", "rule"}, "internalError": nil}

// FixedArgs returns the number of fixed site arguments of an operation
// before the named ones: the receiver, the identifier, the instant, the actor.
func FixedArgs(op Op) int {
	switch op {
	case OpNotify, OpCmd, OpCmdOpt, OpCallOut, OpSchedule, OpMethod, OpTestQuery, OpExpectNotify, OpExpectMatch:
		return 1
	case OpTestCmd:
		return 2
	}
	return 0
}

// Disassemble prints a program as text (reasonr build --dump-bytecode).
func Disassemble(w io.Writer, p *Program) error {
	d := &disasm{p: p}
	if p.Schema.Extension != "" {
		fmt.Fprintf(&d.b, "# extension %s\n\n", p.Schema.Extension)
	}
	if len(p.Consts) > 0 {
		d.b.WriteString("consts\n")
		for i, c := range p.Consts {
			fmt.Fprintf(&d.b, "  k%d %s %s\n", i, d.typ(c.Type), d.lit(c))
		}
	}
	for i := range p.Funcs {
		d.b.WriteByte('\n')
		d.fn(&p.Funcs[i])
	}
	if len(p.Reactions) > 0 {
		d.b.WriteString("\nreactions\n")
		for _, r := range p.Reactions {
			names := make([]string, len(r.Funcs))
			for i, fi := range r.Funcs {
				names[i] = d.funcName(fi)
			}
			fmt.Fprintf(&d.b, "  %s: %s\n", d.method(r.Type, r.Event), strings.Join(names, ", "))
		}
	}
	_, err := io.WriteString(w, d.b.String())
	return err
}

// String returns the disassembled program.
func (p *Program) String() string {
	var b strings.Builder
	_ = Disassemble(&b, p)
	return b.String()
}

type disasm struct {
	p *Program
	b strings.Builder
	f *Func
}

func (d *disasm) typeName(t model.TypeRef) string {
	if ti := d.p.Schema.Type(t); ti != nil {
		return ti.Name
	}
	return "T" + strconv.Itoa(int(t))
}

// typ prints a schema type.
func (d *disasm) typ(t model.Type) string {
	var s string
	switch t.Kind {
	case model.KindEnum, model.KindRecord:
		s = d.typeName(t.Ref)
		if t.Ref == 0 {
			s = "caller"
		}
	case model.KindID:
		s = d.typeName(t.Ref) + ".Id"
	case model.KindTimerID:
		s = "Timer.Id"
	case model.KindPageRequest:
		s = "PageRequest"
	case model.KindList, model.KindSet, model.KindPage:
		s = t.Kind.String() + "<?>"
		if t.Elem != nil {
			s = t.Kind.String() + "<" + d.typ(*t.Elem) + ">"
		}
	case model.KindMap:
		s = "map<?>"
		if t.Key != nil && t.Elem != nil {
			s = "map<" + d.typ(*t.Key) + ", " + d.typ(*t.Elem) + ">"
		}
	default:
		s = t.Kind.String()
	}
	if t.Optional {
		s += "?"
	}
	return s
}

func (d *disasm) lit(c Const) string {
	switch c.Lit.Kind {
	case model.KindNone:
		return "none"
	case model.KindString:
		return strconv.Quote(c.Lit.Text)
	}
	return c.Lit.Text
}

func (d *disasm) funcName(i int32) string {
	if i >= 0 && int(i) < len(d.p.Funcs) {
		return d.p.Funcs[i].Name
	}
	return "f" + strconv.Itoa(int(i))
}

func (d *disasm) method(t model.TypeRef, m model.MethodRef) string {
	ti := d.p.Schema.Type(t)
	if ti == nil || int(m) >= len(ti.Methods) {
		return fmt.Sprintf("T%d.m%d", t, m)
	}
	return ti.Name + "." + ti.Methods[m].Name
}

func (d *disasm) fn(f *Func) {
	d.f = f
	b := &d.b
	fmt.Fprintf(b, "func %s %s", f.Kind, f.Name)
	if f.Kind != FuncTest && f.Kind != FuncMigration && f.Kind != FuncCheck {
		b.WriteByte('(')
		for i := 0; i < f.Params && i < len(f.Regs); i++ {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(b, "%s %s", f.RegNames[i], d.typ(f.Regs[i]))
		}
		b.WriteByte(')')
	}
	if f.Result != nil {
		fmt.Fprintf(b, " %s", d.typ(*f.Result))
	}
	b.WriteByte('\n')
	if len(f.Regs) > 0 {
		items := make([]string, len(f.Regs))
		for i, t := range f.Regs {
			items[i] = "r" + strconv.Itoa(i)
			if i < len(f.RegNames) && f.RegNames[i] != "" {
				items[i] += " " + f.RegNames[i]
			}
			items[i] += " " + d.typ(t)
		}
		fmt.Fprintf(b, "  regs: %s\n", strings.Join(items, ", "))
	}
	if len(f.Consts) > 0 {
		items := make([]string, len(f.Consts))
		for i, k := range f.Consts {
			items[i] = fmt.Sprintf("r%d = k%d", len(f.Regs)+i, k)
		}
		fmt.Fprintf(b, "  consts: %s\n", strings.Join(items, ", "))
	}
	line := 0
	for pc := 0; pc < len(f.Code); {
		for line < len(f.Lines) && int(f.Lines[line].PC) <= pc {
			if int(f.Lines[line].PC) == pc {
				file, n := d.p.Position(f, pc)
				fmt.Fprintf(b, "        # %s:%d\n", file, n)
			}
			line++
		}
		in, ok := DecodeInstr(f.Code, pc)
		if !ok {
			fmt.Fprintf(b, "  %4d  ? %#08x\n", pc, f.Code[pc])
			pc++
			continue
		}
		fmt.Fprintf(b, "  %4d  %s\n", pc, strings.TrimRight(d.instr(in), " "))
		pc += in.Size
	}
}

func (d *disasm) reg(r uint32) string {
	if r == NoReg {
		return "none"
	}
	return "r" + strconv.FormatUint(uint64(r), 10)
}

// field prints a field of type t; IDField is id.
func (d *disasm) field(t model.TypeRef, f model.FieldRef) string {
	if f == IDField {
		return "id"
	}
	if ti := d.p.Schema.Type(t); ti != nil && int(f) < len(ti.Fields) {
		return ti.Fields[f].Name
	}
	return "#" + strconv.Itoa(int(f))
}

func (d *disasm) instr(in Instr) string {
	info := in.Op.Info()
	var ops []string
	vals := [3]uint32{in.A, in.B, in.C}
	for i, role := range info.Roles {
		v := vals[i]
		switch role {
		case RoleDst, RoleReg:
			ops = append(ops, d.reg(v))
		case RoleField:
			ops = append(ops, d.field(d.f.Owner, model.FieldRef(v)))
		case RoleRegField:
			t := model.TypeRef(0)
			if int(in.B) < len(d.f.Regs) {
				t = d.f.Regs[in.B].Ref
			}
			ops = append(ops, d.field(t, model.FieldRef(v)))
		case RoleType:
			ops = append(ops, d.typeName(model.TypeRef(v)))
		case RoleFeature:
			if int(v) < len(d.p.Schema.Features) {
				ops = append(ops, d.p.Schema.Features[v].Name)
			} else {
				ops = append(ops, "feature?"+strconv.Itoa(int(v)))
			}
		case RoleTarget:
			ops = append(ops, strconv.FormatUint(uint64(v), 10))
		case RoleSite:
			ops = append(ops, d.site(in.Op, v))
		case RoleFind:
			ops = append(ops, d.find(v))
		}
	}
	return fmt.Sprintf("%-14s %s", info.Name, strings.Join(ops, ", "))
}

func (d *disasm) site(op Op, i uint32) string {
	if int(i) >= len(d.f.Sites) {
		return "site?" + strconv.Itoa(int(i))
	}
	s := &d.f.Sites[i]
	fixed := min(FixedArgs(op), len(s.Args))
	pre, args := s.Args[:fixed], s.Args[fixed:]
	switch op {
	case OpCollList, OpCollSet, OpCollMap:
		items := make([]string, len(args))
		for j, a := range args {
			items[j] = d.reg(a)
		}
		return "[" + strings.Join(items, ", ") + "]"
	case OpIntrinsic:
		name := "?"
		if s.Func >= 0 && int(s.Func) < len(Intrinsics) {
			name = Intrinsics[s.Func]
		}
		items := make([]string, len(args))
		for j, a := range args {
			items[j] = d.reg(a)
		}
		return name + "(" + strings.Join(items, ", ") + ")"
	}
	// Argument names: the parameters of the method, the fields of the type or of the built-in error.
	var callee string
	var names []string
	switch {
	case s.Method != NoMethod:
		callee = d.method(s.Type, s.Method)
		if ti := d.p.Schema.Type(s.Type); ti != nil && int(s.Method) < len(ti.Methods) {
			for _, p := range ti.Methods[s.Method].Params {
				names = append(names, p.Name)
			}
		}
	case s.Type != 0:
		callee = d.typeName(s.Type)
		if ti := d.p.Schema.Type(s.Type); ti != nil {
			for _, f := range ti.Fields {
				names = append(names, f.Name)
			}
		}
	default:
		callee, names = s.Name, BuiltinErrors[s.Name]
	}
	items := make([]string, len(args))
	for j, a := range args {
		n := "?"
		switch {
		case s.Fields != nil && j < len(s.Fields) && s.Fields[j] == IDField:
			n = "id"
		case s.Fields != nil && j < len(s.Fields) && int(s.Fields[j]) < len(names):
			n = names[s.Fields[j]]
		case s.Fields == nil && j < len(names):
			n = names[j]
		}
		items[j] = n + ": " + d.reg(a)
	}
	call := callee + "(" + strings.Join(items, ", ") + ")"
	switch op {
	case OpNotify, OpExpectNotify:
		return call + " to " + d.reg(pre[0]) + d.subjects(s, names)
	case OpCmd, OpCmdOpt:
		return "[" + d.reg(pre[0]) + "]." + call + d.funcRef(s)
	case OpCallOut:
		return "[" + d.reg(pre[0]) + "]." + call
	case OpSchedule:
		return "at " + d.reg(pre[0]) + ", " + call + d.funcRef(s)
	case OpMethod:
		return d.reg(pre[0]) + "." + call + d.funcRef(s)
	case OpTestCmd:
		if pre[0] == NoReg {
			return call
		}
		return "[" + d.reg(pre[0]) + "]." + call + " as " + d.as(pre[1], s.Role)
	case OpTestQuery:
		return call + " as " + d.as(pre[0], s.Role)
	case OpExpectMatch:
		return d.reg(pre[0]) + " matches " + call
	case OpValNew, OpEvent, OpQueryCall, OpRuleCall:
		return call + d.funcRef(s)
	}
	return call + d.subjects(s, names)
}

// subjects prints argument subjects: " subjects(phone: Customer self.3)".
func (d *disasm) subjects(s *Site, names []string) string {
	if len(s.Subjects) == 0 {
		return ""
	}
	items := make([]string, len(s.Subjects))
	for i, ss := range s.Subjects {
		arg := "?"
		if int(ss.Arg) < len(names) {
			arg = names[ss.Arg]
		}
		path := "self"
		for _, f := range ss.Path {
			path += "." + strconv.Itoa(int(f))
		}
		items[i] = arg + ": " + d.typeName(ss.Type) + " " + path
	}
	return " subjects(" + strings.Join(items, ", ") + ")"
}

// funcRef prints the called function, if any.
func (d *disasm) funcRef(s *Site) string {
	if s.Func == NoFunc {
		return ""
	}
	return " → " + d.funcName(s.Func)
}

func (d *disasm) as(actor uint32, role string) string {
	if actor != NoReg {
		return d.reg(actor)
	}
	if role != "" {
		return role
	}
	return "?"
}

func (d *disasm) path(t model.TypeRef, p model.FieldPath) string {
	parts := make([]string, len(p))
	owner := t
	for i, f := range p {
		parts[i] = d.field(owner, f)
		next := model.TypeRef(0)
		if ti := d.p.Schema.Type(owner); ti != nil && f != IDField && int(f) < len(ti.Fields) &&
			ti.Fields[f].Type.Kind == model.KindRecord {
			next = ti.Fields[f].Type.Ref
		}
		owner = next
	}
	return strings.Join(parts, ".")
}

var condNames = [...]string{model.OpEq: "==", model.OpNe: "!=", model.OpLt: "<", model.OpLe: "<=",
	model.OpGt: ">", model.OpGe: ">=", model.OpIn: "in"}

func (d *disasm) find(i uint32) string {
	if int(i) >= len(d.f.Finds) {
		return "find?" + strconv.Itoa(int(i))
	}
	f := &d.f.Finds[i]
	var b strings.Builder
	b.WriteString(d.typeName(f.Type))
	if ti := d.p.Schema.Type(f.Type); ti != nil && f.Index >= 0 && int(f.Index) < len(ti.Indexes) {
		b.WriteString(" by " + ti.Indexes[f.Index].Name)
	}
	for j, c := range f.Where {
		if j == 0 {
			b.WriteString(" where ")
		} else {
			b.WriteString(" and ")
		}
		op := "?"
		if int(c.Op) < len(condNames) {
			op = condNames[c.Op]
		}
		fmt.Fprintf(&b, "%s %s %s", d.path(f.Type, c.Field), op, d.reg(c.Value))
	}
	for j, k := range f.Sort {
		if j == 0 {
			b.WriteString(" sort ")
		} else {
			b.WriteString(", ")
		}
		b.WriteString(d.path(f.Type, k.Field))
		if k.Desc {
			b.WriteString(" desc")
		}
	}
	if f.Page != NoReg {
		b.WriteString(" page " + d.reg(f.Page))
	}
	if f.Limit != NoReg {
		b.WriteString(" limit " + d.reg(f.Limit))
	}
	return b.String()
}
