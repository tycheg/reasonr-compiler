// Package ir describes the linear typed IR (LIR) of ReasonR
// (RFC §28): functions of blocks with
// instructions over numbered slots. The IR is close to bytecode but stays
// readable: types, methods and fields are named, and the numbers (TypeRef,
// FieldRef…) are assigned by the schema and bytecode generation.
//
// Package lower builds the IR, Print prints it, Encode and Decode serialize it.
package ir

import "sync"

// Program is the IR of an effective model: the base one or one with an extension.
type Program struct {
	Extension string // "" is the base model
	// Files are the source files relative to the module directory:
	// instruction positions refer to them (Instr.File).
	Files []string
	Funcs []*Func
	// Reactions are on reactions by event in declaration order.
	Reactions []*Reaction

	mu      sync.Mutex
	byName  map[funcKey]*Func // index of Func
	indexed int
}

// Reaction is the reactions to an event: functions of kind FuncReaction.
type Reaction struct {
	Event string   // "Order.placed"
	Funcs []string // names of the reaction functions
}

// Func is an IR function.
type Func struct {
	Kind  FuncKind
	Name  string // "Order.place", "Order.on.placed", "Money.check", a test name
	Owner string // the type that owns the function; "" for migrations and tests
	// Access is the access list of public commands and queries.
	Access []Access
	// Params is the number of parameters: they take the first slots.
	Params int
	Slots  []Slot
	Result *Type // nil: no result
	Consts []Const
	Blocks []*Block // b0 is the entry
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

// Access is an access list element.
type Access struct {
	Kind AccessKind
	Name string // a field, actor or role name; "" for self
}

// AccessKind is the kind of an access element.
type AccessKind uint8

const (
	AccessSelf  AccessKind = iota // the actor itself
	AccessField                   // the actor from an entity field
	AccessActor                   // any actor of the type
	AccessRole                    // a role
)

var accessKindNames = [...]string{AccessSelf: "self", AccessField: "field", AccessActor: "actor", AccessRole: "role"}

func (k AccessKind) String() string {
	if int(k) < len(accessKindNames) {
		return accessKindNames[k]
	}
	return "?"
}

// Slot is a function slot: a parameter, a variable or a temporary value.
type Slot struct {
	Name string // the parameter or variable name; "" for temporaries
	Type *Type
}

// Block is a basic block: the last instruction is a terminator (br, jmp, ret, ret.val).
type Block struct {
	Instrs []Instr
}

// Operand is an operand: a slot (>= 0) or a constant from the function pool (< 0).
type Operand int32

// NoOperand: there is no operand (an optional fixed operand).
const NoOperand Operand = -1 << 31

// SlotOp returns a slot operand.
func SlotOp(slot int) Operand { return Operand(slot) }

// ConstOp returns a constant operand by its pool index.
func ConstOp(i int) Operand { return Operand(-1 - i) }

// IsSlot reports whether the operand refers to a slot.
func (o Operand) IsSlot() bool { return o >= 0 }

// IsConst reports whether the operand refers to a constant.
func (o Operand) IsConst() bool { return o < 0 && o != NoOperand }

// Slot returns the slot number.
func (o Operand) Slot() int { return int(o) }

// Const returns the constant index in the pool.
func (o Operand) Const() int { return int(-1 - o) }

// NoSlot: the instruction has no result.
const NoSlot = -1

// Instr is an instruction. The operation table sets its operands: first the
// fixed operands (Fixed), then named arguments (one name in Names per
// operand) or positional ones.
type Instr struct {
	Op      Op
	Dst     int       // the result slot or NoSlot
	Args    []Operand // operands
	Sym     string    // a type, method, field or built-in function
	Names   []string  // argument names or a field path
	Aux     string    // the role in as (test steps)
	Targets []int     // br: [if true, if false]; jmp: [target]
	Find    *FindSpec // find
	// Subjects are the personal data subjects of notify, error and request
	// arguments (RFC §20).
	Subjects []Subject
	// File and Line are the position of the source statement the instruction
	// comes from: a number in Program.Files from 1 and a line from 1; 0 if unknown.
	File, Line int
}

// Subject is the personal data subject in argument Arg: an entity of type
// Type whose id lies in the fields of self at Path (nil is self itself).
type Subject struct {
	Arg  string
	Type string
	Path []string
}

// FindSpec is an index search template (RFC §30.4): the structure is known
// at compile time; condition values, the page and the limit are operands.
type FindSpec struct {
	Type  string
	Index string
	Where []Cond
	Sort  []SortKey
	Page  Operand // NoOperand when absent
	Limit Operand // NoOperand when absent
}

// Cond is a search condition: path op value.
type Cond struct {
	Path  []string
	Op    CondOp
	Value Operand
}

// CondOp is a search condition operator.
type CondOp uint8

const (
	CondEq CondOp = iota
	CondLt
	CondLe
	CondGt
	CondGe
	CondIn
)

var condOpNames = [...]string{CondEq: "==", CondLt: "<", CondLe: "<=", CondGt: ">", CondGe: ">=", CondIn: "in"}

func (o CondOp) String() string {
	if int(o) < len(condOpNames) {
		return condOpNames[o]
	}
	return "?"
}

// SortKey is a sort key.
type SortKey struct {
	Path []string
	Desc bool
}

// Func returns a function by kind and name, or nil. The index is built on
// the first lookup and rebuilt if the number of functions changed
// (docs/plans/0015: a scan per call is quadratic).
func (p *Program) Func(kind FuncKind, name string) *Func {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.byName == nil || p.indexed != len(p.Funcs) {
		p.byName, p.indexed = make(map[funcKey]*Func, len(p.Funcs)), len(p.Funcs)
		for _, f := range p.Funcs {
			if k := (funcKey{f.Kind, f.Name}); p.byName[k] == nil { // the first one, as with a scan
				p.byName[k] = f
			}
		}
	}
	return p.byName[funcKey{kind, name}]
}

type funcKey struct {
	kind FuncKind
	name string
}
