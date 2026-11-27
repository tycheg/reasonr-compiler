package ir

// Op is an instruction operation.
type Op uint16

// OpInfo is the form of an instruction.
type OpInfo struct {
	Name string
	Dst  bool // has a result
	// Fixed is the number of fixed operands; after them come positional
	// operands with Variadic, named ones with Named (names in Instr.Names).
	Fixed    int
	Variadic bool
	Named    bool
	Term     bool // a block terminator
	Targets  int  // number of target blocks
}

// Operations without typed variants.
const (
	OpMove Op = iota
	// Data.
	OpField      // r = field name: a field of the snapshot, the state (in apply) or the receiver (in value methods)
	OpAfterField // r = after.field name: a state field after the emitted events
	OpAfter      // r = after: the state record after the events
	OpSetField   // set name, x: assignment to a field in apply
	OpRecGet     // r = rec.get x, name: a record field
	OpValNew     // r = val.new Type(field: x, …): all fields in declaration order
	OpValGet     // r = val.get x, name: a value object field
	OpCollList   // r = coll.list x…
	OpCollSet    // r = coll.set x…
	OpCollMap    // r = coll.map k, v, …
	OpLen        // r = coll.len xs (loops)
	OpGet        // r = coll.get xs, i (loops)
	OpAppend     // r = coll.append xs, x (select)
	OpPageWith   // r = page.with p, items: a page with other items
	OpInList     // r = in.list x, xs
	OpInSet      // r = in.set x, xs
	OpInMap      // r = in.map x, m: whether the key exists
	OpIsNone     // r = opt.isnone x
	OpNot        // r = not x
	OpIDConv     // r = id.conv Type x: projection ↔ entity identifier
	// Control.
	OpBr        // br x, b1, b2
	OpJmp       // jmp b
	OpRet       // ret
	OpRetVal    // ret.val x
	OpLoopGuard // loop.guard: iteration counter for the engine limit
	// Effects.
	OpEvent       // event Type.event(argument: x, …)
	OpNotify      // notify [to] Type.notification(…); to is NoOperand: to the initiator
	OpError       // error Type.error(…)
	OpCmd         // cmd [x] Type.command(…)
	OpCmdOpt      // cmd? [x] Type.command(…): does nothing if x is none
	OpCallOut     // callout [x] Type.command(…): a model API call from a fake, after commit, from the system role
	OpUpsert      // proj.upsert Projection(id: x, field: y, …): only the passed fields
	OpProjDelete  // proj.delete Projection x
	OpSchedule    // r = schedule at, Type.command(…): a timer for a command of its own entity
	OpTimerDelete // timer.delete x (none does nothing)
	OpRequest     // request System.request(…)
	OpForget      // forget Type x
	OpReact       // react: on reactions to the events this function emitted, in order
	// Reading.
	OpRefExists // r = ref.exists Type x
	OpRefLoad   // r = ref.load Type x: [x] in a command, a reference or none
	OpRecLoad   // r = rec.load Type x: [x] in a query, a record or none
	OpFind      // r = find spec
	OpQueryCall // r = query.call Type.query(…)
	OpAll       // r = all Type: identifiers of all entities (migrations)
	OpInnerAll  // r = inner.all Type: records of an inner entity of its command's aggregate
	// Built-in.
	OpNow       // r = now
	OpToday     // r = today
	OpID        // r = id
	OpNewID     // r = newid Type
	OpHasErrors // r = haserrors
	OpMe        // r = me
	OpCaller    // r = caller
	OpFeature   // r = feature flag: whether feature flag Sym is on for the caller
	OpSelf      // r = self: the receiver of a value object method
	OpRuleCall  // r = rule.call Type.rule(…)
	OpMethod    // r = method.call Type.method x(…)
	OpIntrinsic // r = intrinsic name(x, …): the standard library
	// Tests.
	OpTestCmd      // test.cmd [x] Type.command(…) as a: a is an actor or NoOperand, the role is in Aux
	OpTestQuery    // r = test.query Type.query(…) as a
	OpTestTime     // test.time d
	OpTestWhen     // test.when: the start of the when section
	OpTestFeature  // test.feature flag: the test turns feature flag Sym on for the whole system
	OpExpectNotify // expect.notify [to] Type.notification(…): the passed arguments
	OpExpectError  // expect.error Type.error(…)
	OpExpectReq    // expect.request System.request(…)
	OpExpectMatch  // expect.matches x Type(field: y, …)
	numFixedOps
)

var ops = []OpInfo{
	OpMove:       {Name: "move", Dst: true, Fixed: 1},
	OpField:      {Name: "field", Dst: true},
	OpAfterField: {Name: "after.field", Dst: true},
	OpAfter:      {Name: "after", Dst: true},
	OpSetField:   {Name: "set", Fixed: 1},
	OpRecGet:     {Name: "rec.get", Dst: true, Fixed: 1},
	OpValNew:     {Name: "val.new", Dst: true, Named: true},
	OpValGet:     {Name: "val.get", Dst: true, Fixed: 1},
	OpCollList:   {Name: "coll.list", Dst: true, Variadic: true},
	OpCollSet:    {Name: "coll.set", Dst: true, Variadic: true},
	OpCollMap:    {Name: "coll.map", Dst: true, Variadic: true},
	OpLen:        {Name: "coll.len", Dst: true, Fixed: 1},
	OpGet:        {Name: "coll.get", Dst: true, Fixed: 2},
	OpAppend:     {Name: "coll.append", Dst: true, Fixed: 2},
	OpPageWith:   {Name: "page.with", Dst: true, Fixed: 2},
	OpInList:     {Name: "in.list", Dst: true, Fixed: 2},
	OpInSet:      {Name: "in.set", Dst: true, Fixed: 2},
	OpInMap:      {Name: "in.map", Dst: true, Fixed: 2},
	OpIsNone:     {Name: "opt.isnone", Dst: true, Fixed: 1},
	OpNot:        {Name: "not", Dst: true, Fixed: 1},
	OpIDConv:     {Name: "id.conv", Dst: true, Fixed: 1},

	OpBr:        {Name: "br", Fixed: 1, Term: true, Targets: 2},
	OpJmp:       {Name: "jmp", Term: true, Targets: 1},
	OpRet:       {Name: "ret", Term: true},
	OpRetVal:    {Name: "ret.val", Fixed: 1, Term: true},
	OpLoopGuard: {Name: "loop.guard"},

	OpEvent:       {Name: "event", Named: true},
	OpNotify:      {Name: "notify", Fixed: 1, Named: true},
	OpError:       {Name: "error", Named: true},
	OpCmd:         {Name: "cmd", Fixed: 1, Named: true},
	OpCmdOpt:      {Name: "cmd?", Fixed: 1, Named: true},
	OpCallOut:     {Name: "callout", Fixed: 1, Named: true},
	OpUpsert:      {Name: "proj.upsert", Named: true},
	OpProjDelete:  {Name: "proj.delete", Fixed: 1},
	OpSchedule:    {Name: "schedule", Dst: true, Fixed: 1, Named: true},
	OpTimerDelete: {Name: "timer.delete", Fixed: 1},
	OpRequest:     {Name: "request", Named: true},
	OpForget:      {Name: "forget", Fixed: 1},
	OpReact:       {Name: "react"},

	OpRefExists: {Name: "ref.exists", Dst: true, Fixed: 1},
	OpRefLoad:   {Name: "ref.load", Dst: true, Fixed: 1},
	OpRecLoad:   {Name: "rec.load", Dst: true, Fixed: 1},
	OpFind:      {Name: "find", Dst: true},
	OpQueryCall: {Name: "query.call", Dst: true, Named: true},
	OpAll:       {Name: "all", Dst: true},
	OpInnerAll:  {Name: "inner.all", Dst: true},

	OpNow:       {Name: "now", Dst: true},
	OpToday:     {Name: "today", Dst: true},
	OpID:        {Name: "id", Dst: true},
	OpNewID:     {Name: "newid", Dst: true},
	OpHasErrors: {Name: "haserrors", Dst: true},
	OpMe:        {Name: "me", Dst: true},
	OpCaller:    {Name: "caller", Dst: true},
	OpFeature:   {Name: "feature", Dst: true},
	OpSelf:      {Name: "self", Dst: true},
	OpRuleCall:  {Name: "rule.call", Dst: true, Named: true},
	OpMethod:    {Name: "method.call", Dst: true, Fixed: 1, Named: true},
	OpIntrinsic: {Name: "intrinsic", Dst: true, Variadic: true},

	OpTestCmd:      {Name: "test.cmd", Fixed: 2, Named: true},
	OpTestQuery:    {Name: "test.query", Dst: true, Fixed: 1, Named: true},
	OpTestTime:     {Name: "test.time", Fixed: 1},
	OpTestWhen:     {Name: "test.when"},
	OpTestFeature:  {Name: "test.feature"},
	OpExpectNotify: {Name: "expect.notify", Fixed: 1, Named: true},
	OpExpectError:  {Name: "expect.error", Named: true},
	OpExpectReq:    {Name: "expect.request", Named: true},
	OpExpectMatch:  {Name: "expect.matches", Fixed: 1, Named: true},
}

// Suffixes of typed operations.
var (
	// arithKinds is arithmetic: integers (int, bigint), float, decimal.
	arithKinds = []string{"i64", "f64", "dec"}
	// eqKinds is equality: all value kinds.
	eqKinds = []string{"i64", "f64", "dec", "str", "bool", "uuid", "date", "time", "ts", "dur", "bytes",
		"enum", "id", "timer", "rec", "list", "set", "map"}
	// ordKinds are the ordered kinds.
	ordKinds = []string{"i64", "f64", "dec", "str", "date", "time", "ts", "dur"}
)

var byName = map[string]Op{}

func init() {
	add := func(name string, fixed int) {
		ops = append(ops, OpInfo{Name: name, Dst: true, Fixed: fixed})
	}
	for _, base := range []string{"add", "sub", "mul", "div", "rem"} {
		for _, k := range arithKinds {
			add(base+"."+k, 2)
		}
	}
	for _, k := range arithKinds {
		add("neg."+k, 1)
	}
	// Strings, durations and time.
	for _, name := range []string{"add.str", "add.dur", "sub.dur", "mul.dur", "add.ts", "sub.ts", "diff.ts"} {
		add(name, 2)
	}
	add("neg.dur", 1)
	for _, base := range []string{"eq", "ne"} {
		for _, k := range eqKinds {
			add(base+"."+k, 2)
		}
	}
	for _, base := range []string{"lt", "le", "gt", "ge"} {
		for _, k := range ordKinds {
			add(base+"."+k, 2)
		}
	}
	for i, info := range ops {
		if info.Name == "" {
			panic("ir: an operation without a name")
		}
		if _, dup := byName[info.Name]; dup {
			panic("ir: repeated operation " + info.Name)
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

// MustLookup returns an operation by name or panics.
func MustLookup(name string) Op {
	op, ok := byName[name]
	if !ok {
		panic("ir: no operation " + name)
	}
	return op
}
