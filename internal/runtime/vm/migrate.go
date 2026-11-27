package vm

import (
	"context"
	"fmt"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
)

// Migrate runs manual migration name (RFC §32, item 3). Every command call
// from the migration function is a separate unit of work: the VM passes its
// result to commit, which commits it before the next call. The results lie
// in the shared migration arena: commit does not call Release. A commit
// error stops the migration.
func (m *Machine) Migrate(ctx context.Context, name string, env model.Env, now time.Time, commit func(*model.Outcome) error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = caught(p)
		}
	}()
	return m.migrate(ctx, name, env, now, commit)
}

func (m *Machine) migrate(ctx context.Context, name string, env model.Env, now time.Time, commit func(*model.Outcome) error) error {
	fi := m.prog.FuncIndex(bytecode.FuncMigration, name)
	if fi == bytecode.NoFunc {
		fi = m.prog.FuncIndex(bytecode.FuncMigration, "migration."+name)
	}
	if fi == bytecode.NoFunc {
		return &Fault{Err: fmt.Errorf("%w: migration %q", ErrNotFound, name)}
	}
	r := m.start(ctx, env, model.Caller{Kind: model.CallerMigration}, nil, now)
	defer func() {
		model.PutArena(r.a)
		r.migrate = nil
		r.finish()
	}()
	r.migrate = commit
	_, err := r.call(fi, nil, frame{})
	return err
}

// all returns the identifiers of all entities of type t in id order (all T
// in migrations): a search without an index or a limit.
func (r *run) all(t model.TypeRef) (model.Value, error) {
	spec := &r.spec
	*spec = model.FindSpec{Type: t, Index: model.NoIndex, Cursor: model.None, Limit: -1}
	rows, err := r.env.Find(r.ctx, r.a, spec)
	if err != nil {
		return model.None, fmt.Errorf("Env.Find: %w", err)
	}
	m := r.a.Mark()
	if rows.Records.Kind == model.KindList {
		for _, rec := range r.a.Elems(rows.Records) {
			if e := r.a.Elems(rec); len(e) > 0 {
				r.a.Push(e[len(e)-1])
			}
		}
	}
	return r.a.End(model.KindList, 0, m), nil
}

// migrationUnit runs a command call from the migration function as a
// separate unit of work and passes its result to commit.
func (r *run) migrationUnit(s *bytecode.Site, regs []model.Value, optional bool) error {
	r.errors = r.errors[:0]
	r.recycle()
	r.emitted, r.sets, r.deferred = r.emitted[:0], r.sets[:0], r.deferred[:0]
	r.nevents, r.seq = 0, 0
	r.projections, r.timers, r.notifications, r.requests, r.forget =
		r.projections[:0], r.timers[:0], r.notifications[:0], r.requests[:0], r.forget[:0]
	id := regs[s.Args[0]]
	r.root = model.CommandCall{Entity: s.Type, ID: id, Command: s.Method}
	r.self = id
	commit := r.migrate
	r.migrate = nil // nested calls are part of this unit of work
	err := r.cmd(&frame{}, s, regs, optional)
	r.migrate = commit
	if err != nil {
		return err
	}
	out := &model.Outcome{Arena: r.a} // not from the pool: the arena is shared by the whole migration
	if len(r.errors) > 0 {
		out.SetErrors(r.errors)
	} else {
		r.work(out)
	}
	return commit(out)
}
