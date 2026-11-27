package ir

import (
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Print prints a program as text (reasonr build --dump-ir).
func Print(w io.Writer, p *Program) error {
	var b strings.Builder
	if p.Extension != "" {
		fmt.Fprintf(&b, "# extension %s\n\n", p.Extension)
	}
	for i, f := range p.Funcs {
		if i > 0 {
			b.WriteByte('\n')
		}
		f.print(&b)
	}
	if len(p.Reactions) > 0 {
		b.WriteString("\nreactions\n")
		for _, r := range p.Reactions {
			fmt.Fprintf(&b, "  %s: %s\n", r.Event, strings.Join(r.Funcs, ", "))
		}
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// String returns the program text.
func (p *Program) String() string {
	var b strings.Builder
	_ = Print(&b, p)
	return b.String()
}

func (f *Func) print(b *strings.Builder) {
	fmt.Fprintf(b, "func %s %s", f.Kind, quoteName(f))
	if f.Kind != FuncTest && f.Kind != FuncMigration && f.Kind != FuncCheck {
		b.WriteByte('(')
		for i := 0; i < f.Params && i < len(f.Slots); i++ {
			if i > 0 {
				b.WriteString(", ")
			}
			fmt.Fprintf(b, "%s %s", f.Slots[i].Name, f.Slots[i].Type)
		}
		b.WriteByte(')')
	}
	if f.Result != nil {
		fmt.Fprintf(b, " %s", f.Result)
	}
	if len(f.Access) > 0 {
		items := make([]string, len(f.Access))
		for i, a := range f.Access {
			items[i] = a.Kind.String()
			if a.Name != "" {
				items[i] += " " + a.Name
			}
		}
		fmt.Fprintf(b, " access=[%s]", strings.Join(items, ", "))
	}
	b.WriteByte('\n')
	if len(f.Slots) > 0 {
		items := make([]string, len(f.Slots))
		for i, s := range f.Slots {
			items[i] = fmt.Sprintf("r%d", i)
			if s.Name != "" {
				items[i] += " " + s.Name
			}
			items[i] += " " + s.Type.String()
		}
		fmt.Fprintf(b, "  slots: %s\n", strings.Join(items, ", "))
	}
	for i, blk := range f.Blocks {
		fmt.Fprintf(b, "  b%d:\n", i)
		for _, in := range blk.Instrs {
			b.WriteString("    ")
			b.WriteString(f.Format(in))
			b.WriteByte('\n')
		}
	}
}

func quoteName(f *Func) string {
	if f.Kind == FuncTest {
		return strconv.Quote(f.Name)
	}
	return f.Name
}

// Operand returns the text of an operand: r3 or a constant.
func (f *Func) Operand(o Operand) string {
	switch {
	case o == NoOperand:
		return "_"
	case o.IsSlot():
		return "r" + strconv.Itoa(o.Slot())
	case o.Const() < len(f.Consts):
		return f.Consts[o.Const()].String()
	}
	return "c?" + strconv.Itoa(o.Const())
}

// Format returns the text of an instruction.
func (f *Func) Format(in Instr) string {
	info := in.Op.Info()
	var b strings.Builder
	if in.Dst != NoSlot {
		fmt.Fprintf(&b, "r%d = ", in.Dst)
	}
	b.WriteString(info.Name)
	if len(in.Args) < info.Fixed || len(in.Targets) != info.Targets {
		fmt.Fprintf(&b, " <invalid instruction>")
		return b.String()
	}
	fixed, rest := in.Args[:info.Fixed], in.Args[info.Fixed:]
	ops := func(list []Operand) string {
		s := make([]string, len(list))
		for i, o := range list {
			s[i] = f.Operand(o)
		}
		return strings.Join(s, ", ")
	}
	named := func() string {
		s := make([]string, len(rest))
		for i, o := range rest {
			name := "?"
			if i < len(in.Names) {
				name = in.Names[i]
			}
			s[i] = name + ": " + f.Operand(o)
		}
		return in.Sym + "(" + strings.Join(s, ", ") + ")"
	}
	switch in.Op {
	case OpBr:
		fmt.Fprintf(&b, " %s, %s", ops(fixed), targets(in.Targets))
	case OpJmp:
		fmt.Fprintf(&b, " %s", targets(in.Targets))
	case OpField, OpAfterField, OpNewID, OpAll, OpInnerAll, OpFeature, OpTestFeature:
		fmt.Fprintf(&b, " %s", in.Sym)
	case OpSetField:
		fmt.Fprintf(&b, " %s, %s", in.Sym, ops(fixed))
	case OpRecGet, OpValGet:
		fmt.Fprintf(&b, " %s, %s", ops(fixed), in.Sym)
	case OpRefExists, OpRefLoad, OpRecLoad, OpIDConv, OpProjDelete, OpForget:
		fmt.Fprintf(&b, " %s %s", in.Sym, ops(fixed))
	case OpCmd, OpCmdOpt, OpCallOut:
		fmt.Fprintf(&b, " [%s] %s", ops(fixed), named())
	case OpNotify, OpExpectNotify:
		fmt.Fprintf(&b, " %s", named())
		if fixed[0] != NoOperand {
			fmt.Fprintf(&b, " to %s", f.Operand(fixed[0]))
		}
	case OpSchedule:
		fmt.Fprintf(&b, " %s, %s", ops(fixed), named())
	case OpMethod, OpExpectMatch:
		fmt.Fprintf(&b, " %s %s", ops(fixed), named())
	case OpTestCmd:
		if fixed[0] == NoOperand {
			fmt.Fprintf(&b, " %s", named())
			break
		}
		fmt.Fprintf(&b, " [%s] %s%s", f.Operand(fixed[0]), named(), as(f, fixed[1], in.Aux))
	case OpTestQuery:
		fmt.Fprintf(&b, " %s%s", named(), as(f, fixed[0], in.Aux))
	case OpIntrinsic:
		fmt.Fprintf(&b, " %s(%s)", in.Sym, ops(in.Args))
	case OpFind:
		if in.Find != nil {
			b.WriteString(" " + f.formatFind(in.Find))
		}
	default:
		switch {
		case info.Named:
			fmt.Fprintf(&b, " %s", named())
		case len(in.Args) > 0:
			fmt.Fprintf(&b, " %s", ops(in.Args))
		}
	}
	b.WriteString(formatSubjects(in.Subjects))
	return b.String()
}

// formatSubjects prints argument subjects: " subjects(phone: Customer self.customer)".
func formatSubjects(ss []Subject) string {
	if len(ss) == 0 {
		return ""
	}
	items := make([]string, len(ss))
	for i, s := range ss {
		items[i] = s.Arg + ": " + s.Type + " " + strings.Join(append([]string{"self"}, s.Path...), ".")
	}
	return " subjects(" + strings.Join(items, ", ") + ")"
}

func targets(t []int) string {
	s := make([]string, len(t))
	for i, x := range t {
		s[i] = "b" + strconv.Itoa(x)
	}
	return strings.Join(s, ", ")
}

func as(f *Func, actor Operand, role string) string {
	switch {
	case actor != NoOperand:
		return " as " + f.Operand(actor)
	case role != "":
		return " as " + role
	}
	return ""
}

func (f *Func) formatFind(s *FindSpec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s by %s", s.Type, s.Index)
	if len(s.Where) > 0 {
		conds := make([]string, len(s.Where))
		for i, c := range s.Where {
			conds[i] = strings.Join(c.Path, ".") + " " + c.Op.String() + " " + f.Operand(c.Value)
		}
		fmt.Fprintf(&b, " where %s", strings.Join(conds, " and "))
	}
	if len(s.Sort) > 0 {
		keys := make([]string, len(s.Sort))
		for i, k := range s.Sort {
			keys[i] = strings.Join(k.Path, ".")
			if k.Desc {
				keys[i] += " desc"
			}
		}
		fmt.Fprintf(&b, " sort %s", strings.Join(keys, ", "))
	}
	if s.Page != NoOperand {
		fmt.Fprintf(&b, " page %s", f.Operand(s.Page))
	}
	if s.Limit != NoOperand {
		fmt.Fprintf(&b, " limit %s", f.Operand(s.Limit))
	}
	return b.String()
}
