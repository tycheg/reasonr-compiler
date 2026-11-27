// Package vm is the ReasonR bytecode interpreter
// (RFC §29): an implementation of
// model.Interpreter. The VM is pure: everything external goes through
// model.Env, values live in the arena of the unit of work, the caller
// sets the order and the time, so execution is deterministic.
//
// Business errors (error …, forbidden, invalidArgument) are a call result.
// A runtime failure (an overflow, division by zero, a check violated inside
// logic, an exceeded limit, an Env error) is a *Fault error.
package vm

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"runtime/debug"
	"strings"
	"sync"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
	"github.com/tycheg/reasonr-compiler/internal/runtime/record"
)

// Limits are the execution limits per unit of work (RFC §24.3). The engine sets them.
type Limits struct {
	Iterations int // loop iterations
	Depth      int // nested call depth
	Events     int
	Timers     int
	Messages   int // notifications and external requests
	// Memory is the bytes in the arena of a unit of work; checked on every
	// loop iteration (collections are copied, and a loop may grow
	// quadratically); 0 means no limit.
	Memory int
}

// DefaultLimits are the default limits (RFC §24.3).
var DefaultLimits = Limits{Iterations: 100_000, Depth: 64, Events: 10_000, Timers: 1_000, Messages: 1_000, Memory: 64 << 20}

// Machine is a loaded program. It is safe for concurrent calls.
type Machine struct {
	prog      *bytecode.Program
	schema    *model.Schema
	static    *model.Arena
	consts    []model.Value
	codec     *record.Codec
	limits    Limits
	queries   map[methodKey]int32
	commands  map[methodKey]int32
	reactions map[methodKey][]int32        // on reactions by event
	checks    map[model.TypeRef]int32      // the check function of a value object
	applies   map[methodKey]int32          // the apply function by event
	code      [][]instr                    // the decoded code of functions by address
	defaults  map[methodKey][]model.Value  // default values of parameters (the static arena)
	inner     map[model.TypeRef]*innerInfo // inner entities of aggregates (inner.go)
	// aclContext is the receiving bounded context of an ACL reaction by function number.
	aclContext map[int32]string
	regexps    sync.Map // string → *regexp.Regexp (or error)
	runs       sync.Pool
}

type methodKey struct {
	t model.TypeRef
	m model.MethodRef
}

// New verifies a program and prepares it for running.
func New(p *bytecode.Program, limits Limits) (*Machine, error) {
	if err := bytecode.Verify(p); err != nil {
		return nil, err
	}
	static, consts, err := p.Constants()
	if err != nil {
		return nil, err
	}
	m := &Machine{
		prog: p, schema: p.Schema, static: static, consts: consts, codec: record.New(p.Schema), limits: limits,
		queries: map[methodKey]int32{}, commands: map[methodKey]int32{}, reactions: map[methodKey][]int32{},
		checks: map[model.TypeRef]int32{}, code: make([][]instr, len(p.Funcs)), defaults: map[methodKey][]model.Value{},
	}
	for i := range p.Funcs {
		m.code[i] = decode(p.Funcs[i].Code)
	}
	m.applies = applyFuncs(p)
	m.applies = applyFuncs(p)
	if err := m.paramDefaults(); err != nil {
		return nil, err
	}
	for _, rc := range p.Reactions {
		m.reactions[methodKey{rc.Type, rc.Event}] = rc.Funcs
	}
	m.initInner()
	m.aclContext = map[int32]string{}
	for i := range p.Funcs {
		if f := &p.Funcs[i]; f.Kind == bytecode.FuncACL {
			if _, cx, ok := strings.Cut(f.Name, "@"); ok {
				m.aclContext[int32(i)] = cx
			}
		}
	}
	for i := range p.Funcs {
		f := &p.Funcs[i]
		switch f.Kind {
		case bytecode.FuncQuery:
			m.queries[methodKey{f.Owner, f.Method}] = int32(i)
		case bytecode.FuncCommand:
			m.commands[methodKey{f.Owner, f.Method}] = int32(i)
		case bytecode.FuncCheck:
			m.checks[f.Owner] = int32(i)
		}
	}
	m.runs.New = func() any { return &run{m: m} }
	return m, nil
}

// instr is a decoded instruction: the VM decodes the code once on load (the
// verifier has checked the program).
type instr struct {
	A, B, C uint32
	Op      bytecode.Op
	Size    uint8
}

// decode decodes the code of a function: an instruction lies at the address
// of its start, the other elements are unused.
func decode(code []uint32) []instr {
	out := make([]instr, len(code))
	for pc := 0; pc < len(code); {
		in, _ := bytecode.DecodeInstr(code, pc)
		out[pc] = instr{A: in.A, B: in.B, C: in.C, Op: in.Op, Size: uint8(in.Size)}
		pc += max(in.Size, 1)
	}
	return out
}

// paramDefaults computes in the static arena the default values of the
// parameters of public commands and queries, so arguments of calls from
// outside do not parse schema literals on every call. none means no value.
func (m *Machine) paramDefaults() error {
	for _, ti := range m.schema.Types {
		for _, mi := range ti.Methods {
			if mi.Kind != model.MethodCommand && mi.Kind != model.MethodQuery {
				continue
			}
			vals := make([]model.Value, len(mi.Params))
			for i := range mi.Params {
				p := &mi.Params[i]
				var err error
				switch {
				case p.Default != nil:
					vals[i], err = m.codec.Literal(m.static, p.Default, p.Type)
				case p.Type.Kind == model.KindPageRequest && !p.Type.Optional:
					vals[i], err = m.codec.Zero(m.static, p.Type) // a default PageRequest has size 20 (RFC §7.2)
				}
				if err != nil {
					return fmt.Errorf("vm: default value %s.%s(%s): %w", ti.Name, mi.Name, p.Name, err)
				}
			}
			m.defaults[methodKey{ti.Ref, mi.Ref}] = vals
		}
	}
	return nil
}

// Schema returns the program's schema.
func (m *Machine) Schema() *model.Schema { return m.schema }

// Program returns the program.
func (m *Machine) Program() *bytecode.Program { return m.prog }

// Fault is a runtime failure: the engine answers internalError, developers
// get the place of the failure and the model call stack.
type Fault struct {
	Func string // the model function
	PC   int    // the instruction address
	Op   string
	File string // the source file relative to the module; "" if unknown
	Line int
	// Stack holds the calling model functions, from the nearest to the outermost.
	Stack []Frame
	Err   error
}

// Frame is a model function call in a failure stack: the address and the call place.
type Frame struct {
	Func string
	PC   int
	File string
	Line int
}

func (f *Fault) Error() string {
	if f.Func == "" {
		return "vm: " + f.Err.Error()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "vm: %s", f.Func)
	if f.File != "" {
		fmt.Fprintf(&b, " (%s:%d)", f.File, f.Line)
	}
	if f.Op != "" {
		fmt.Fprintf(&b, " %s", f.Op)
	}
	fmt.Fprintf(&b, ": %v", f.Err)
	for _, fr := range f.Stack {
		fmt.Fprintf(&b, "\n\tcalled from %s", fr.Func)
		if fr.File != "" {
			fmt.Fprintf(&b, " (%s:%d)", fr.File, fr.Line)
		}
	}
	return b.String()
}

func (f *Fault) Unwrap() error { return f.Err }

// Failure causes.
var (
	ErrOverflow     = errors.New("overflow")
	ErrDivZero      = errors.New("division by zero")
	ErrCheck        = errors.New("value object check violated")
	ErrLimit        = errors.New("execution limit exceeded")
	ErrConvert      = errors.New("cannot convert the value")
	ErrUnsupported  = errors.New("the operation is not available here")
	ErrNotFound     = errors.New("no such command or query")
	ErrInvalidRegex = errors.New("invalid regular expression")
	// ErrInternal is a panic inside the interpreter: a VM bug, not a model one.
	ErrInternal = errors.New("internal interpreter error")
)

// caught turns a panic inside the VM into a runtime failure: an arena
// overflow is an exceeded limit, anything else an internal VM error.
func caught(p any) *Fault {
	if e, ok := p.(error); ok && errors.Is(e, model.ErrArenaFull) {
		return &Fault{Err: fmt.Errorf("%w: %v", ErrLimit, e)}
	}
	return &Fault{Err: fmt.Errorf("%w: %v\n%s", ErrInternal, p, debug.Stack())}
}

// regexp returns a compiled expression (a full string match).
func (m *Machine) regexp(src string) (*regexp.Regexp, error) {
	if v, ok := m.regexps.Load(src); ok {
		if re, ok := v.(*regexp.Regexp); ok {
			return re, nil
		}
		return nil, ErrInvalidRegex
	}
	re, err := regexp.Compile(`^(?:` + src + `)$`)
	if err != nil {
		m.regexps.Store(src, err)
		return nil, ErrInvalidRegex
	}
	m.regexps.Store(src, re)
	return re, nil
}

// Query runs a public query called from outside: it checks access and the
// arguments, then runs the query function.
func (m *Machine) Query(ctx context.Context, call model.QueryCall, env model.Env) (out *model.QueryOutcome, err error) {
	defer func() {
		if p := recover(); p != nil {
			out, err = nil, caught(p)
		}
	}()
	return m.query(ctx, call, env)
}

func (m *Machine) query(ctx context.Context, call model.QueryCall, env model.Env) (*model.QueryOutcome, error) {
	fi, ok := m.queries[methodKey{call.Type, call.Query}]
	if !ok {
		return nil, &Fault{Err: fmt.Errorf("%w: query %d.%d", ErrNotFound, call.Type, call.Query)}
	}
	r := m.start(ctx, env, call.Caller, call.Arena, call.Now)
	defer r.finish()
	mi := &m.schema.Type(call.Type).Methods[call.Query]
	result := model.None
	if !r.allowed(mi, model.None) {
		r.builtinError("forbidden")
	} else {
		args, ok, err := r.arguments(methodKey{call.Type, call.Query}, mi.Params, call.Args, call.Arena)
		if ok && err == nil {
			result, err = r.call(fi, args, frame{})
		}
		if err != nil {
			model.PutArena(r.a)
			return nil, err
		}
	}
	out := model.NewQueryOutcome(r.a)
	if len(r.errors) > 0 {
		out.SetErrors(r.errors)
	} else {
		out.Result = result
	}
	return out, nil
}

// allowed checks the access of a call from outside (RFC §19, decision 5 of
// the plan): any of the conditions. Timers and migrations are not checked.
// state is the entity record for access by field.
func (r *run) allowed(mi *model.MethodInfo, state model.Value) bool {
	c := &r.caller
	if c.Kind == model.CallerTimer || c.Kind == model.CallerMigration || c.Kind == model.CallerFake {
		return true
	}
	for _, acc := range mi.Access {
		switch acc.Kind {
		case model.AccessAny:
			return true
		case model.AccessActor:
			if c.Actor == acc.Actor && c.Actor != 0 || r.actsAs(acc.Actor) {
				return true
			}
		case model.AccessRole:
			for _, role := range c.Roles {
				if role == acc.Role {
					return true
				}
			}
		case model.AccessSelf:
			if c.Actor != 0 && r.self.Kind == model.KindID && (c.Actor == r.self.Type || r.actsAs(r.self.Type)) && r.a.Equal(r.subject, r.self) {
				return true
			}
		case model.AccessField:
			if state.Kind == model.KindRecord && len(acc.Field) > 0 && c.Actor != 0 {
				v := state
				for _, f := range acc.Field {
					if v.Kind != model.KindRecord || int(f) >= len(r.a.Elems(v)) {
						v = model.None
						break
					}
					v = r.a.Elems(v)[f]
				}
				if v.Kind == model.KindID && v.Type != r.subject.Type {
					r.actsAs(v.Type)
				}
				if r.a.Equal(v, r.subject) {
					return true
				}
			}
		}
	}
	return false
}

// builtinError adds a built-in error without arguments.
func (r *run) builtinError(name string) {
	r.errors = append(r.errors, model.BusinessError{Name: name, Args: r.a.Record(0)})
}

// invalidArgument adds an invalidArgument(path, rule) error.
func (r *run) invalidArgument(path, rule string) {
	r.errors = append(r.errors, model.BusinessError{
		Name: "invalidArgument", Args: r.a.Record(0, r.a.String(path), r.a.String(rule)),
	})
}

// actsAs translates a caller that is an actor of another bounded context
// into actor t through an ACL (actor Auth.User as Customer,
// docs/plans/0011): the same identifier with type t. From then on the
// caller is that actor, and me is its id.
func (r *run) actsAs(t model.TypeRef) bool {
	c := &r.caller
	ti := r.m.schema.Type(t)
	if c.Actor == 0 || c.Actor == t || ti == nil || !ti.ActsAs(c.Actor) {
		return false
	}
	r.subject.Type = t
	c.Actor = t
	return true
}
