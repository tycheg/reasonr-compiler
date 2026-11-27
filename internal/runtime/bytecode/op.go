package bytecode

// Op is an operation code: the low byte of an instruction word.
type Op uint8

// Format is the operand layout in an instruction word.
type Format uint8

const (
	// FormatNone: no operands.
	FormatNone Format = iota
	// FormatABC: three 8-bit operands (16-bit with the WIDE prefix).
	FormatABC
	// FormatABx: operand A (8 bits) and Bx (16 bits); 16 and 32 with WIDE.
	FormatABx
)

// Role is the purpose of an operand.
type Role uint8

const (
	RoleNone     Role = iota
	RoleDst           // the result register
	RoleReg           // an operand register
	RoleField         // a field of the function's type (snapshot, state, receiver)
	RoleRegField      // a field of a record or value object from register B
	RoleType          // a schema type number
	RoleSite          // a number in Func.Sites
	RoleFind          // a number in Func.Finds
	RoleTarget        // a jump address
	RoleFeature       // a schema feature flag number (Schema.Features)
)

// OpInfo is the form of an operation.
type OpInfo struct {
	Name   string
	Format Format
	Roles  [3]Role // A, B, C; A and Bx for FormatABx
	Term   bool    // execution does not continue with the next instruction
}

// op builds a form from a string of roles: d is the result, r a register,
// f a field of the function's type, g a record field from B, t a type, s a
// site, x a find, j a jump, n a feature flag, _ unused.
func op(name, roles string) OpInfo {
	info := OpInfo{Name: name, Format: FormatABC, Term: name == "jmp" || name == "ret" || name == "ret.val"}
	for i, c := range roles {
		info.Roles[i] = map[rune]Role{'d': RoleDst, 'r': RoleReg, 'f': RoleField, 'g': RoleRegField, 't': RoleType,
			's': RoleSite, 'x': RoleFind, 'j': RoleTarget, 'n': RoleFeature, '_': RoleNone}[c]
		if c == 's' || c == 'x' || c == 'j' {
			info.Format = FormatABx
		}
	}
	if roles == "" {
		info.Format = FormatNone
	}
	return info
}

// Operations. Registers are r*, numbers in function tables are site
// (Func.Sites) and find (Func.Finds); fields and types are schema numbers.
const (
	OpWide Op = iota // a prefix: the high bytes of the next instruction's operands

	OpMove       // A = B
	OpField      // A = field B of the snapshot (in apply of the state, in value methods of the receiver)
	OpAfterField // A = field B of the state after the events
	OpAfter      // A = the state after the events
	OpSet        // field A = B (apply)
	OpRecGet     // A = field C of record B
	OpRecID      // A = the identifier of record B (an entity or a projection)
	OpValGet     // A = field C of value object B
	OpValNew     // A = a value object by site Bx (all fields in order)
	OpCollList   // A = a list of the arguments of site Bx
	OpCollSet    // A = a set of the arguments of site Bx
	OpCollMap    // A = a map of the arguments of site Bx (key, value, …)
	OpLen        // A = the length of collection B
	OpGet        // A = element B[C]
	OpAppend     // A = B with element C at the end
	OpPageWith   // A = page B with elements C
	OpInList     // A = B in list C
	OpInSet      // A = B in set C
	OpInMap      // A = key B in map C
	OpIsNone     // A = B is none
	OpNot        // A = not B
	OpIDConv     // A = identifier B as an identifier of type C

	OpJmp       // jump to Bx
	OpBrTrue    // jump to Bx if A is true
	OpBrFalse   // jump to Bx if A is false
	OpRet       // return
	OpRetVal    // return value A
	OpLoopGuard // a loop iteration: the engine limit

	OpEvent       // an event by site Bx
	OpNotify      // a notification by site Bx; argument 0 is the recipient or NoReg
	OpError       // an error by site Bx
	OpCmd         // a command by site Bx; argument 0 is the entity identifier
	OpCmdOpt      // like cmd, but does nothing if the identifier is none
	OpCallOut     // a model API call from a fake by site Bx: argument 0 is the identifier; the engine runs it after commit
	OpUpsert      // a projection upsert by site Bx; Fields are the passed fields
	OpProjDelete  // delete the row of projection C with identifier A
	OpSchedule    // A = a timer by site Bx; argument 0 is the instant
	OpTimerDelete // delete timer A (none does nothing)
	OpRequest     // an external request by site Bx
	OpForget      // forget subject A of type C
	OpReact       // on reactions to the events this function emitted

	OpRefExists // A = whether entity C with identifier B exists
	OpRefLoad   // A = a reference to entity C with identifier B, or none
	OpRecLoad   // A = the record of entity or projection C with identifier B, or none
	OpFind      // A = the result of a search by template Bx
	OpQueryCall // A = the result of a query by site Bx
	OpAll       // A = the identifiers of all entities of type C (migrations)
	OpInnerAll  // A = the records of inner entity C of its command's aggregate

	OpNow       // A = now
	OpToday     // A = today
	OpID        // A = id
	OpNewID     // A = a new identifier of type C
	OpHasErrors // A = whether there are errors
	OpMe        // A = me
	OpCaller    // A = caller
	OpFeature   // A = whether feature flag B is on for the caller
	OpSelf      // A = the receiver of a value object method
	OpRuleCall  // A = a rule by site Bx
	OpMethod    // A = a value object method by site Bx; argument 0 is the receiver
	OpIntrinsic // A = the built-in function of site Bx (Site.Func is the number in Intrinsics)

	OpTestCmd      // a test step: a command by site Bx; arguments 0, 1 are the entity (NoReg for the fake instance) and the as actor (or NoReg, then the role Site.Role)
	OpTestQuery    // A = a test step: a query by site Bx; argument 0 is the as actor
	OpTestTime     // a test step: time A passed
	OpTestWhen     // the start of the when section
	OpTestFeature  // the test turns feature flag B on for the whole system
	OpExpectNotify // a notification by site Bx is expected; argument 0 is the recipient or NoReg
	OpExpectError  // an error by site Bx is expected
	OpExpectReq    // an external request by site Bx is expected
	OpExpectMatch  // the value of argument 0 of site Bx matches in the fields Site.Fields

	numFixedOps
)

var ops = []OpInfo{
	OpWide:         op("wide", "___"),
	OpMove:         op("move", "dr"),
	OpField:        op("field", "df"),
	OpAfterField:   op("after.field", "df"),
	OpAfter:        op("after", "d"),
	OpSet:          op("set", "fr"),
	OpRecGet:       op("rec.get", "drg"),
	OpRecID:        op("rec.id", "dr"),
	OpValGet:       op("val.get", "drg"),
	OpValNew:       op("val.new", "ds"),
	OpCollList:     op("coll.list", "ds"),
	OpCollSet:      op("coll.set", "ds"),
	OpCollMap:      op("coll.map", "ds"),
	OpLen:          op("coll.len", "dr"),
	OpGet:          op("coll.get", "drr"),
	OpAppend:       op("coll.append", "drr"),
	OpPageWith:     op("page.with", "drr"),
	OpInList:       op("in.list", "drr"),
	OpInSet:        op("in.set", "drr"),
	OpInMap:        op("in.map", "drr"),
	OpIsNone:       op("opt.isnone", "dr"),
	OpNot:          op("not", "dr"),
	OpIDConv:       op("id.conv", "drt"),
	OpJmp:          op("jmp", "_j"),
	OpBrTrue:       op("br.true", "rj"),
	OpBrFalse:      op("br.false", "rj"),
	OpRet:          op("ret", ""),
	OpRetVal:       op("ret.val", "r"),
	OpLoopGuard:    op("loop.guard", ""),
	OpEvent:        op("event", "_s"),
	OpNotify:       op("notify", "_s"),
	OpError:        op("error", "_s"),
	OpCmd:          op("cmd", "_s"),
	OpCmdOpt:       op("cmd?", "_s"),
	OpCallOut:      op("callout", "_s"),
	OpUpsert:       op("proj.upsert", "_s"),
	OpProjDelete:   op("proj.delete", "r_t"),
	OpSchedule:     op("schedule", "ds"),
	OpTimerDelete:  op("timer.delete", "r"),
	OpRequest:      op("request", "_s"),
	OpForget:       op("forget", "r_t"),
	OpReact:        op("react", ""),
	OpRefExists:    op("ref.exists", "drt"),
	OpRefLoad:      op("ref.load", "drt"),
	OpRecLoad:      op("rec.load", "drt"),
	OpFind:         op("find", "dx"),
	OpQueryCall:    op("query.call", "ds"),
	OpAll:          op("all", "d_t"),
	OpInnerAll:     op("inner.all", "d_t"),
	OpNow:          op("now", "d"),
	OpToday:        op("today", "d"),
	OpID:           op("id", "d"),
	OpNewID:        op("newid", "d_t"),
	OpHasErrors:    op("haserrors", "d"),
	OpMe:           op("me", "d"),
	OpCaller:       op("caller", "d"),
	OpFeature:      op("feature", "dn"),
	OpSelf:         op("self", "d"),
	OpRuleCall:     op("rule.call", "ds"),
	OpMethod:       op("method.call", "ds"),
	OpIntrinsic:    op("intrinsic", "ds"),
	OpTestCmd:      op("test.cmd", "_s"),
	OpTestQuery:    op("test.query", "ds"),
	OpTestTime:     op("test.time", "r"),
	OpTestWhen:     op("test.when", ""),
	OpTestFeature:  op("test.feature", "_n"),
	OpExpectNotify: op("expect.notify", "_s"),
	OpExpectError:  op("expect.error", "_s"),
	OpExpectReq:    op("expect.request", "_s"),
	OpExpectMatch:  op("expect.matches", "_s"),
}

// Typed operations: A = B op C (unary ones A = op B). Operand kinds: i32
// is int (a 32-bit overflow is a failure), i64 bigint, f64, dec, str, dur,
// ts and the other value kinds for comparisons.
var (
	ArithKinds = []string{"i32", "i64", "f64", "dec"}
	EqKinds    = []string{"i32", "i64", "f64", "dec", "str", "bool", "uuid", "date", "time", "ts", "dur", "bytes",
		"enum", "id", "timer", "rec", "list", "set", "map"}
	OrdKinds = []string{"i32", "i64", "f64", "dec", "str", "date", "time", "ts", "dur"}
	// MixedOps are operations on time and strings: add.str, add.dur, sub.dur,
	// mul.dur (duration × integer), add.ts, sub.ts (instant ± duration),
	// diff.ts (difference of instants), neg.dur.
	MixedOps = []string{"add.str", "add.dur", "sub.dur", "mul.dur", "add.ts", "sub.ts", "diff.ts"}
)

var byName = map[string]Op{}

func init() {
	add := func(name string) {
		roles := "drr"
		if name[:4] == "neg." {
			roles = "dr"
		}
		ops = append(ops, op(name, roles))
	}
	for _, base := range []string{"add", "sub", "mul", "div", "rem"} {
		for _, k := range ArithKinds {
			add(base + "." + k)
		}
	}
	for _, k := range ArithKinds {
		add("neg." + k)
	}
	for _, name := range MixedOps {
		add(name)
	}
	add("neg.dur")
	for _, base := range []string{"eq", "ne"} {
		for _, k := range EqKinds {
			add(base + "." + k)
		}
	}
	for _, base := range []string{"lt", "le", "gt", "ge"} {
		for _, k := range OrdKinds {
			add(base + "." + k)
		}
	}
	if len(ops) > 256 {
		panic("bytecode: more than 256 operations")
	}
	for i, info := range ops {
		if info.Name == "" {
			panic("bytecode: an operation without a name")
		}
		if _, dup := byName[info.Name]; dup {
			panic("bytecode: repeated operation " + info.Name)
		}
		byName[info.Name] = Op(i)
	}
}

// Info returns the form of an operation.
func (op Op) Info() OpInfo {
	if int(op) < len(ops) {
		return ops[op]
	}
	return OpInfo{Name: "?"}
}

func (op Op) String() string { return op.Info().Name }

// Valid reports whether the operation is known.
func (op Op) Valid() bool { return int(op) < len(ops) }

// Lookup returns an operation by name.
func Lookup(name string) (Op, bool) {
	op, ok := byName[name]
	return op, ok
}

// NumFixedOps is the number of untyped operations: typed ones come after.
func NumFixedOps() int { return int(numFixedOps) }

// NumOps is the number of operations.
func NumOps() int { return len(ops) }

// ---------------------------------------------------------------------------
// Word encoding.

// Instr is a decoded instruction.
type Instr struct {
	Op      Op
	A, B, C uint32 // for FormatABx, Bx lies in B
	Wide    bool
	Size    int // words: 1, or 2 with the WIDE prefix
}

// DecodeInstr decodes the instruction at address pc. ok == false: the
// address is outside the code, the operation is unknown or WIDE has no instruction.
func DecodeInstr(code []uint32, pc int) (Instr, bool) {
	if pc < 0 || pc >= len(code) {
		return Instr{}, false
	}
	w := code[pc]
	var hi uint32
	in := Instr{Size: 1}
	if Op(w) == OpWide {
		if pc+1 >= len(code) || Op(code[pc+1]) == OpWide {
			return Instr{}, false
		}
		hi, in.Wide, in.Size = w, true, 2
		w = code[pc+1]
	}
	in.Op = Op(w)
	if !in.Op.Valid() {
		return Instr{}, false
	}
	switch in.Op.Info().Format {
	case FormatABC:
		in.A = w >> 8 & 0xff
		in.B = w >> 16 & 0xff
		in.C = w >> 24
		if in.Wide {
			in.A |= (hi >> 8 & 0xff) << 8
			in.B |= (hi >> 16 & 0xff) << 8
			in.C |= (hi >> 24) << 8
		}
	case FormatABx:
		in.A = w >> 8 & 0xff
		in.B = w >> 16
		if in.Wide {
			in.A |= (hi >> 8 & 0xff) << 8
			in.B |= (hi >> 16) << 16
		}
	case FormatNone:
		if w>>8 != 0 || in.Wide {
			return Instr{}, false
		}
	}
	return in, true
}

// AppendInstr appends an instruction to the code, with the WIDE prefix for
// large operands. ok == false: an operand does not fit even with WIDE.
func AppendInstr(code []uint32, op Op, a, b, c uint32) ([]uint32, bool) {
	switch op.Info().Format {
	case FormatNone:
		return append(code, uint32(op)), a == 0 && b == 0 && c == 0
	case FormatABC:
		if a > 0xffff || b > 0xffff || c > 0xffff {
			return code, false
		}
		if a > 0xff || b > 0xff || c > 0xff {
			code = append(code, uint32(OpWide)|(a>>8)<<8|(b>>8)<<16|(c>>8)<<24)
		}
		return append(code, uint32(op)|(a&0xff)<<8|(b&0xff)<<16|(c&0xff)<<24), true
	default: // FormatABx
		if a > 0xffff || c != 0 {
			return code, false
		}
		if a > 0xff || b > 0xffff {
			code = append(code, uint32(OpWide)|(a>>8)<<8|(b>>16)<<16)
		}
		return append(code, uint32(op)|(a&0xff)<<8|(b&0xffff)<<16), true
	}
}
