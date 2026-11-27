// Package bytecode is the format of a compiled ReasonR program: functions
// of 32-bit instructions over registers, call and search tables, a constant
// pool and the model schema (RFC §29).
// internal/codegen builds a program from IR, the VM runs it.
//
// An instruction is a word: the low byte is the operation, then operands
// A, B, C of 8 bits (FormatABC) or A and a 16-bit Bx (FormatABx). A WIDE
// prefix before an instruction carries the high bytes of its operands: 16
// bits for A, B, C and 32 for Bx.
//
// Frame registers: first the IR slots (parameters first), then the
// function's constants (Func.Consts), which the VM copies into the frame on
// entry. Jumps go by word number in the function code.
package bytecode

import (
	"fmt"
	"sort"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/record"
)

// FormatVersion is the serialization format version.
const FormatVersion = 5

// Program is a model program: the base one or one with an extension.
type Program struct {
	Schema *model.Schema
	// Files are the source files relative to the module directory:
	// Func.Lines positions refer to them (numbered from 1).
	Files     []string
	Consts    []Const // the program's constant pool
	Funcs     []Func
	Reactions []Reaction // on reactions by event, in declaration order
}

// Const is a constant: the value of a literal of type Type. Lit.Kind == KindNone is none.
type Const struct {
	Type model.Type
	Lit  model.Literal
}

// FuncKind is the kind of a function.
type FuncKind uint8

const (
	FuncCommand   FuncKind = iota
	FuncApply              // applying an event to the state
	FuncQuery              // a query
	FuncRule               // a rule
	FuncMethod             // a value object method
	FuncCheck              // the check invariants of a value object: a bool result
	FuncReaction           // an on reaction of an extension
	FuncMigration          // a manual migration
	FuncTest               // an acceptance test
	FuncUI                 // a layout expression: an if condition, an argument (RFC §22)
	FuncACL                // an ACL reaction to an event of the source bounded context (docs/plans/0011)
)

var funcKindNames = [...]string{
	FuncCommand: "command", FuncApply: "apply", FuncQuery: "query", FuncRule: "rule", FuncMethod: "method",
	FuncCheck: "check", FuncReaction: "on", FuncMigration: "migration", FuncTest: "test", FuncUI: "ui",
	FuncACL: "acl",
}

func (k FuncKind) String() string {
	if int(k) < len(funcKindNames) {
		return funcKindNames[k]
	}
	return "?"
}

// Special number values.
const (
	NoReg    = ^uint32(0)          // no argument (an optional operand)
	NoFunc   = int32(-1)           // no function
	NoMethod = ^model.MethodRef(0) // no method
	// IDField is the id "field" of an entity or projection record in search
	// paths, upsert and matches.
	IDField = ^model.FieldRef(0)
)

// Func is a function.
type Func struct {
	Name   string // "Order.place", "Order.on.placed", "Money.check", a test name
	Kind   FuncKind
	Owner  model.TypeRef   // 0 for migrations and tests
	Method model.MethodRef // the schema method; for a reaction its event; NoMethod when none
	Params int             // parameters are the first registers
	// Regs are the types of slot registers (without constants), RegNames the
	// names of parameters and variables ("" for temporaries) for debugging.
	Regs     []model.Type
	RegNames []string
	Consts   []uint32 // numbers in Program.Consts: registers len(Regs)…
	Result   *model.Type
	Code     []uint32
	Sites    []Site
	Finds    []Find
	// Lines are source positions: from address PC to the next entry the code
	// comes from line Line of file File (a number in Program.Files from 1).
	// The entries are ordered by PC.
	Lines []Line
}

// Line is an entry of a function's position table.
type Line struct {
	PC, File, Line uint32
}

// Position returns the file and line of the instruction at address pc; ""
// and 0 mean the position is unknown.
func (p *Program) Position(f *Func, pc int) (string, int) {
	i := sort.Search(len(f.Lines), func(i int) bool { return int(f.Lines[i].PC) > pc }) - 1
	if i < 0 {
		return "", 0
	}
	l := f.Lines[i]
	if l.File == 0 || int(l.File) > len(p.Files) {
		return "", 0
	}
	return p.Files[l.File-1], int(l.Line)
}

// Frame returns the frame size: slots and constants.
func (f *Func) Frame() int { return len(f.Regs) + len(f.Consts) }

// Site is a call table entry: everything that does not fit into the word of
// an instruction with a variable number of operands.
type Site struct {
	Type   model.TypeRef   // the type of the method, value object or projection
	Method model.MethodRef // NoMethod when none
	Func   int32           // the called function (for intrinsic, the number in Intrinsics); NoFunc when none
	Args   []uint32        // argument registers; NoReg when none
	// Fields are the numbers of the passed fields or parameters: upsert,
	// expect.* and matches do not pass all of them. nil means all in order.
	Fields []model.FieldRef
	Name   string // the name of a built-in error (forbidden…) for expect.error with Type == 0
	Role   string // test.cmd, test.query: the as role
	// Subjects are the personal data subjects of notify, error and request
	// arguments (RFC §20).
	Subjects []SiteSubject
}

// SiteSubject is the personal data subject in an argument: parameter
// number Arg, the subject type and the path to its id in the fields of self
// (empty for self itself).
type SiteSubject struct {
	Arg  int32
	Type model.TypeRef
	Path model.FieldPath
}

// Find is a search template: the structure is known at compile time;
// condition values, the page and the limit are registers.
type Find struct {
	Type  model.TypeRef
	Index int32 // the index number; -1 for a search without an index
	Where []Cond
	Sort  []model.SortKey
	Page  uint32 // the PageRequest register or NoReg
	Limit uint32 // the limit register or NoReg
}

// Cond is a search condition.
type Cond struct {
	Field model.FieldPath
	Op    model.CondOp
	Value uint32 // a register
}

// Reaction is the on reactions to an event.
type Reaction struct {
	Type  model.TypeRef
	Event model.MethodRef
	Funcs []int32
}

// Constants builds the values of the constant pool in a static arena: the
// VM copies them into function frames.
func (p *Program) Constants() (*model.Arena, []model.Value, error) {
	a := model.NewStaticArena()
	c := record.New(p.Schema)
	out := make([]model.Value, len(p.Consts))
	for i := range p.Consts {
		k := &p.Consts[i]
		t := k.Type
		if k.Lit.Kind == model.KindNone {
			t.Optional = true
		}
		v, err := c.Literal(a, &k.Lit, t)
		if err != nil {
			return nil, nil, fmt.Errorf("bytecode: constant %d: %w", i, err)
		}
		out[i] = v
	}
	return a, out, nil
}

// FuncIndex returns the number of a function by kind and name, or NoFunc.
func (p *Program) FuncIndex(kind FuncKind, name string) int32 {
	for i := range p.Funcs {
		if p.Funcs[i].Kind == kind && p.Funcs[i].Name == name {
			return int32(i)
		}
	}
	return NoFunc
}
