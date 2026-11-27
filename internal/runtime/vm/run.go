package vm

import (
	"context"
	"fmt"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
	"github.com/tycheg/reasonr-compiler/internal/runtime/dec"
)

// run is the state of one call from outside (a unit of work or a query).
// Function frames are windows of a shared register stack.
type run struct {
	m       *Machine
	ctx     context.Context
	env     model.Env
	a       *model.Arena
	caller  model.Caller
	subject model.Value // the id of the calling actor in the arena
	self    model.Value // the id of the root command's entity (self access)
	now     model.Value
	errors  []model.BusinessError
	stack   []model.Value
	args    []model.Value // the arguments of a nested call
	params  []model.Value // the arguments of the call from outside
	iters   int
	depth   int
	spec    model.FindSpec
	conds   []model.Cond
	callerV model.Value // caller as a value (lazily)
	quote   bool        // text: strings in quotes, identifiers by name (test messages)
	test    *testRun    // an acceptance test is running
	// migrate commits a unit of work of a migration: every command call from
	// the migration function is a separate unit of work.
	migrate func(*model.Outcome) error

	// The unit of work.
	root          model.CommandCall
	ents          []*entity
	spare         []*entity // entities of earlier calls for reuse
	sets          []model.FieldValue
	ibuf          []model.Value // arguments of built-in functions
	kbuf          []byte        // the key of an external request
	emitted       []emitted
	deferred      []deferred // reactions of entity models in other bounded contexts: after the root command
	nevents       int
	seq           uint32
	projections   []model.ProjectionOp
	timers        []model.TimerOp
	notifications []model.Notification
	requests      []model.ExternalRequest
	forget        []model.Subject
	calls         []model.FakeCall
}

// start takes a state from the pool and the arena of the unit of work.
func (m *Machine) start(ctx context.Context, env model.Env, caller model.Caller, src *model.Arena, now time.Time) *run {
	r := m.runs.Get().(*run)
	r.ctx, r.env = ctx, env
	r.a = model.GetArena()
	r.a.SetStatic(m.static)
	r.caller = caller
	r.subject = r.importValue(src, caller.Subject)
	r.self = model.None
	r.now = model.Timestamp(now)
	r.errors = r.errors[:0]
	r.stack = r.stack[:0]
	r.iters, r.depth = 0, 0
	r.callerV = model.None
	r.root = model.CommandCall{}
	r.ents, r.emitted, r.sets, r.ibuf = r.ents[:0], r.emitted[:0], r.sets[:0], r.ibuf[:0]
	r.deferred = r.deferred[:0]
	r.nevents, r.seq = 0, 0
	r.projections, r.timers, r.notifications, r.requests, r.forget =
		r.projections[:0], r.timers[:0], r.notifications[:0], r.requests[:0], r.forget[:0]
	r.calls = r.calls[:0]
	return r
}

// finish returns the state to the pool; the arena belongs to the result.
func (r *run) finish() {
	r.ctx, r.env, r.a = nil, nil, nil
	r.caller = model.Caller{}
	clear(r.errors)
	r.recycle()
	clear(r.emitted)
	r.m.runs.Put(r)
}

// recycle sends the entities of the unit of work to the spare list.
func (r *run) recycle() {
	r.spare = append(r.spare, r.ents...)
	clear(r.ents)
	r.ents = r.ents[:0]
}

// importValue copies an engine value into the arena of the unit of work.
func (r *run) importValue(src *model.Arena, v model.Value) model.Value {
	if src == nil {
		if v.Ref.Len != 0 {
			return model.None // the reference has nothing to point to: the engine passed no arena
		}
		return v
	}
	return r.a.Copy(src, v)
}

// arguments copies the arguments of a call from outside into the arena and
// checks them against the parameters: omitted ones (none for a non-optional
// type) get their default value, otherwise invalidArgument. ok == false
// means there are errors.
func (r *run) arguments(key methodKey, params []model.FieldInfo, args model.Record, src *model.Arena) ([]model.Value, bool, error) {
	defaults := r.m.defaults[key]
	var given []model.Value
	if src != nil && args.Ref.Len > 0 {
		given = src.Fields(args)
	}
	out := r.params[:0]
	ok := true
	for i := range params {
		p := &params[i]
		v := model.None
		if i < len(given) {
			v = r.importValue(src, given[i])
		}
		if v.Kind == model.KindNone && i < len(defaults) {
			v = defaults[i] // the default value or none
		}
		path, rule, err := r.validate(v, &p.Type)
		if err != nil {
			return nil, false, err
		}
		if rule != "" {
			r.invalidArgument(p.Name+path, rule)
			ok = false
		}
		out = append(out, v)
	}
	if len(given) > len(params) {
		r.invalidArgument("", "count")
		ok = false
	}
	r.params = out
	return out, ok, nil
}

// maxPage is the largest page size (RFC §17).
const maxPage = 100

// validate checks an argument value by type. A violation is a rule
// (required, type, range, encoding, check) and a path inside the value
// (.field, [i]); rule == "" means the value is valid. error is a failure in check.
func (r *run) validate(v model.Value, t *model.Type) (path, rule string, err error) {
	if v.Kind == model.KindNone {
		if !t.Optional {
			return "", "required", nil
		}
		return "", "", nil
	}
	if v.Kind != t.Kind {
		return "", "type", nil
	}
	s := r.m.schema
	switch t.Kind {
	case model.KindInt:
		if x := v.Int(); x != int64(int32(x)) {
			return "", "range", nil
		}
	case model.KindDecimal:
		if _, ok := dec.FromBits(v.Lo, v.Hi); !ok {
			return "", "range", nil
		}
	case model.KindBool:
		if v.Lo > 1 {
			return "", "type", nil
		}
	case model.KindString:
		if !utf8.Valid(r.a.Data(v)) {
			return "", "encoding", nil
		}
	case model.KindTime:
		if v.Lo >= uint64(24*time.Hour) {
			return "", "range", nil
		}
	case model.KindTimestamp:
		if v.Hi >= 1e9 {
			return "", "range", nil
		}
	case model.KindEnum:
		ti := s.Type(t.Ref)
		if v.Type != t.Ref || ti == nil || v.Lo >= uint64(len(ti.Enum)) {
			return "", "type", nil
		}
	case model.KindID:
		if v.Type != t.Ref {
			return "", "type", nil
		}
	case model.KindRecord:
		ti := s.Type(t.Ref)
		if v.Type != t.Ref || ti == nil || int(v.Ref.Len) != len(ti.Fields) {
			return "", "type", nil
		}
		for i, f := range r.a.Elems(v) {
			if p, rule, err := r.validate(f, &ti.Fields[i].Type); rule != "" || err != nil {
				return "." + ti.Fields[i].Name + p, rule, err
			}
		}
		if fi, has := r.m.checks[t.Ref]; has {
			res, err := r.call(fi, nil, frame{self: v})
			if err != nil {
				return "", "", err
			}
			if !res.Bool() {
				return "", "check", nil
			}
		}
	case model.KindList, model.KindSet, model.KindMap:
		for i, e := range r.a.Elems(v) {
			et := t.Elem
			if t.Kind == model.KindMap && i%2 == 0 {
				et = t.Key
			}
			if p, rule, err := r.validate(e, et); rule != "" || err != nil {
				n := i
				if t.Kind == model.KindMap {
					n = i / 2
				}
				return "[" + strconv.Itoa(n) + "]" + p, rule, err
			}
		}
	case model.KindPageRequest:
		e := r.a.Elems(v)
		if len(e) != 2 || e[0].Kind != model.KindInt || e[1].Kind != model.KindNone && e[1].Kind != model.KindString {
			return "", "type", nil
		}
		if n := e[0].Int(); n < 1 || n > maxPage {
			return ".size", "range", nil
		}
	}
	return "", "", nil
}

// call calls function fi: args are the parameters, fr the method receiver,
// the checked value of check, the snapshot and the entity of a command.
func (r *run) call(fi int32, args []model.Value, fr frame) (model.Value, error) {
	f := &r.m.prog.Funcs[fi]
	if r.depth >= r.m.limits.Depth {
		return model.None, &Fault{Func: f.Name, Err: fmt.Errorf("%w: call depth %d", ErrLimit, r.m.limits.Depth)}
	}
	r.depth++
	base := len(r.stack)
	n := f.Frame()
	r.stack = append(r.stack, make([]model.Value, n)...)
	regs := r.stack[base : base+n]
	copy(regs, args[:min(len(args), f.Params)])
	for i, k := range f.Consts {
		regs[len(f.Regs)+i] = r.m.consts[k]
	}
	emitStart := len(r.emitted)
	res, err := r.exec(f, r.m.code[fi], base, &fr)
	r.emitted = r.emitted[:emitStart]
	clear(r.stack[base:])
	r.stack = r.stack[:base]
	r.depth--
	return res, err
}

// fault builds a runtime failure at instruction pc of function f.
func (r *run) fault(f *bytecode.Func, pc int, op bytecode.Op, err error) *Fault {
	file, line := r.m.prog.Position(f, pc)
	return &Fault{Func: f.Name, PC: pc, Op: op.String(), File: file, Line: line, Err: err}
}
