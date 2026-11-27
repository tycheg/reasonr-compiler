package vm

import (
	"context"
	"errors"
	"path/filepath"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/check"
	"github.com/tycheg/reasonr-compiler/internal/codegen"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/lower"
	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/rfcblocks"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
	"github.com/tycheg/reasonr-compiler/internal/runtime/record"
	"github.com/tycheg/reasonr-compiler/internal/schemagen"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

// memEnv is a stub Env with prepared records: Load and Find by scanning.
type memEnv struct {
	s     *model.Schema
	codec *record.Codec
	a     *model.Arena // the storage
	rows  map[model.TypeRef][]stored
	fail  error // the error of all calls
	ids   int
}

type stored struct{ id, state model.Value }

func newEnv(s *model.Schema) *memEnv {
	return &memEnv{s: s, codec: record.New(s), a: model.NewArena(), rows: map[model.TypeRef][]stored{}}
}

func (e *memEnv) Load(_ context.Context, a *model.Arena, t model.TypeRef, id model.Value) (model.Record, uint64, bool, error) {
	if e.fail != nil {
		return model.Record{}, 0, false, e.fail
	}
	for _, r := range e.rows[t] {
		if model.Equal(e.a, r.id, a, id) {
			return a.Copy(e.a, r.state).Record(), 1, true, nil
		}
	}
	d, err := e.codec.Default(a, t)
	return d, 0, false, err
}

func (e *memEnv) Find(_ context.Context, a *model.Arena, q *model.FindSpec) (model.Rows, error) {
	if e.fail != nil {
		return model.Rows{}, e.fail
	}
	var found []stored
	for _, r := range e.rows[q.Type] {
		ok := true
		for _, c := range q.Where {
			ok = ok && e.match(e.get(r, c.Field), c.Op, e.a.Copy(a, c.Value))
		}
		if ok {
			found = append(found, r)
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		for _, k := range q.Sort {
			c := e.a.Compare(e.get(found[i], k.Field), e.get(found[j], k.Field))
			if k.Desc {
				c = -c
			}
			if c != 0 {
				return c < 0
			}
		}
		return false
	})
	start, n := 0, q.Limit
	if n < 0 {
		n = len(found) // all T: no limit
	}
	next := model.None
	if q.Page {
		if q.Cursor.Kind == model.KindString {
			start, _ = strconv.Atoi(a.Text(q.Cursor))
		}
		n = q.Size
		if start+n < len(found) {
			next = a.String(strconv.Itoa(start + n))
		}
	}
	found = found[min(start, len(found)):]
	found = found[:min(n, len(found))]
	m := a.Mark()
	for _, r := range found {
		mr := a.Mark()
		for _, f := range e.a.Elems(r.state) {
			a.Push(a.Copy(e.a, f))
		}
		a.Push(a.Copy(e.a, r.id))
		a.Push(a.End(model.KindRecord, q.Type, mr))
	}
	return model.Rows{Records: a.End(model.KindList, 0, m), Next: next}, nil
}

// NewID issues deterministic identifiers: a uuid with a counter (high byte
// 0xee), a sequence is a counter per type.
func (e *memEnv) NewID(_ context.Context, a *model.Arena, t model.TypeRef) (model.Value, error) {
	if e.fail != nil {
		return model.None, e.fail
	}
	e.ids++
	u := [16]byte{0: 0xee, 15: byte(e.ids)}
	if t == 0 {
		return model.TimerID(u), nil
	}
	switch e.s.Type(t).ID {
	case model.IDUUID:
		return model.ID(t, model.UUID(u)), nil
	case model.IDSequence:
		return model.ID(t, model.Bigint(int64(e.ids))), nil
	}
	return model.None, errors.New("the caller sets the identifier")
}

func (e *memEnv) get(r stored, path model.FieldPath) model.Value {
	v := r.state
	for _, f := range path {
		if f == bytecode.IDField {
			v = r.id
			continue
		}
		v = e.a.Elems(v)[f]
	}
	return v
}

func (e *memEnv) match(x model.Value, op model.CondOp, y model.Value) bool {
	switch op {
	case model.OpEq:
		return e.a.Equal(x, y)
	case model.OpNe:
		return !e.a.Equal(x, y)
	case model.OpIn:
		for _, v := range e.a.Elems(y) {
			if e.a.Equal(x, v) {
				return true
			}
		}
		return false
	}
	if x.Kind == model.KindNone {
		return false
	}
	c := e.a.Compare(x, y)
	switch op {
	case model.OpLt:
		return c < 0
	case model.OpLe:
		return c <= 0
	case model.OpGt:
		return c > 0
	}
	return c >= 0
}

// fixture is a machine of an example model and a stub Env.
type fixture struct {
	t   *testing.T
	m   *Machine
	s   *model.Schema
	env *memEnv
	now time.Time
}

func load(t *testing.T, dir, ext string) *fixture {
	t.Helper()
	mod, diags, err := module.Load(source.NewFileSet(), dir)
	if err != nil || diag.HasErrors(diags) {
		t.Fatal(err, diags)
	}
	p, diags, err := mod.Program(ext)
	if err != nil {
		t.Fatal(err)
	}
	info, d := sema.Check(p)
	res := check.Analyze(p, info, check.All)
	if diag.HasErrors(append(append(diags, d...), res.Diagnostics...)) {
		t.Fatal("the model has errors")
	}
	s := schemagen.Generate(p, info)
	bc, err := codegen.Generate(lower.Program(p, info, res), s)
	if err != nil {
		t.Fatal(err)
	}
	m, err := New(bc, DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, m: m, s: s, env: newEnv(s), now: time.Date(2026, 10, 4, 12, 0, 0, 0, time.UTC)}
}

func example(name string) string { return filepath.Join(rfcblocks.Examples(), name) }

func (f *fixture) typ(name string) model.TypeRef {
	ti := f.s.Lookup(name)
	if ti == nil {
		f.t.Fatalf("no type %s", name)
	}
	return ti.Ref
}

func (f *fixture) enum(typ, value string) model.Value {
	ti := f.s.Lookup(typ)
	for i, v := range ti.Enum {
		if v == value {
			return model.Enum(ti.Ref, i)
		}
	}
	f.t.Fatalf("no value %s.%s", typ, value)
	return model.None
}

// id is a uuid identifier of the type with number n.
func (f *fixture) id(typ string, n byte) model.Value {
	return model.ID(f.typ(typ), model.UUID([16]byte{15: n}))
}

// value builds a value object in arena a: default values and fields fields.
func (f *fixture) value(a *model.Arena, typ string, fields map[string]model.Value) model.Value {
	t := f.typ(typ)
	r, err := f.env.codec.Default(a, t)
	if err != nil {
		// a value object with an identifier has no default value
		ti := f.s.Type(t)
		vals := make([]model.Value, len(ti.Fields))
		r = a.Record(t, vals...)
	}
	vals := a.Fields(r)
	for name, v := range fields {
		fi := f.s.Type(t).Field(name)
		if fi == nil {
			f.t.Fatalf("no field %s.%s", typ, name)
		}
		vals[fi.Ref] = v
	}
	return r.Value()
}

// put stores an entity or projection record; for an entity with models in
// several bounded contexts, the model that has all fields fields.
func (f *fixture) put(typ string, id model.Value, fields map[string]model.Value) {
	t := f.typ(typ)
	f.env.rows[t] = append(f.env.rows[t], stored{id: id, state: f.value(f.env.a, typ, fields)})
}

// as is a caller: an actor (typ, id) or a role.
func (f *fixture) actor(typ string, id model.Value) model.Caller {
	return model.Caller{Kind: model.CallerUser, Subject: id, Actor: f.typ(typ)}
}

func role(name string) model.Caller {
	return model.Caller{Kind: model.CallerUser, Roles: []string{name}}
}

// caller is a caller with schema roles (admin), as after authentication.
func (f *fixture) caller(c model.Caller) model.Caller {
	if len(c.Roles) > 0 {
		roles := make([]string, len(c.Roles))
		for i, r := range c.Roles {
			roles[i] = f.s.Role(r)
		}
		c.Roles = roles
	}
	return c
}

// query calls a public query; the arguments are in the storage arena.
func (f *fixture) query(typ, name string, c model.Caller, args ...model.Value) (*model.QueryOutcome, error) {
	ti, mi := f.s.Method(typ, name)
	if mi == nil {
		f.t.Fatalf("no query %s.%s", typ, name)
	}
	t := ti.Ref
	c = f.caller(c)
	return f.m.Query(context.Background(), model.QueryCall{
		Type: t, Query: mi.Ref, Args: f.env.a.Record(0, args...), Arena: f.env.a, Caller: c, Now: f.now,
	}, f.env)
}

// show prints a query result: a value or errors.
func (f *fixture) show(out *model.QueryOutcome) string {
	r := &run{m: f.m, a: out.Arena}
	if len(out.Errors) > 0 {
		s := out.Arena.StringStart()
		for i, e := range out.Errors {
			if i > 0 {
				out.Arena.StringAppend([]byte("; "))
			}
			out.Arena.StringAppend([]byte("error " + e.Name))
			if len(out.Arena.Fields(e.Args)) > 0 {
				r.list(e.Args.Value())
			}
		}
		return out.Arena.Text(out.Arena.StringEnd(s))
	}
	s := out.Arena.StringStart()
	r.text(out.Result)
	return out.Arena.Text(out.Arena.StringEnd(s))
}

// call calls a model function directly (rules, methods, check).
func (f *fixture) call(kind bytecode.FuncKind, name string, self model.Value, args ...model.Value) (string, error) {
	fi := f.m.prog.FuncIndex(kind, name)
	if fi == bytecode.NoFunc {
		f.t.Fatalf("no function %s %s", kind, name)
	}
	r := f.m.start(context.Background(), f.env, model.Caller{}, nil, f.now)
	defer r.finish()
	a := r.a
	for i := range args {
		args[i] = a.Copy(f.env.a, args[i])
	}
	self = a.Copy(f.env.a, self)
	v, err := r.call(fi, args, frame{self: self})
	if err != nil {
		return "", err
	}
	s := a.StringStart()
	r.text(v)
	return a.Text(a.StringEnd(s)), nil
}
