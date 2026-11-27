package model

import (
	"context"
	"time"
)

// Interpreter runs a compiled program: the base model or a tenant model
// (RFC §30.2). An error is a runtime failure: the engine answers
// internalError. Results refer to the arena of the unit of work and are
// valid until Release.
type Interpreter interface {
	Schema() *Schema
	Execute(ctx context.Context, call CommandCall, env Env) (*Outcome, error)
	Query(ctx context.Context, call QueryCall, env Env) (*QueryOutcome, error)
}

// CallerKind is who calls a command or a query.
type CallerKind uint8

const (
	CallerUser      CallerKind = iota // a person or an external system with a token
	CallerTimer                       // a fired timer
	CallerMigration                   // a manual migration
	CallerTest                        // the test runner
	CallerFake                        // an external system fake: the implementation of a request the engine delivered to it
)

// Caller is an authenticated caller. The interpreter checks access (RFC
// §24.3): for CallerUser calls at the start of a public command or query; a
// violation is the forbidden error.
type Caller struct {
	Kind    CallerKind
	Subject Value   // the actor id if the caller is an actor; None otherwise
	Actor   TypeRef // the actor type; 0 when not an actor
	Roles   []string
	Tenant  string // the engine fills it; the interpreter does not use it
	// Features are the feature flags on for the caller: the engine computes
	// them once per unit of work (RFC §23.4).
	Features Features
}

// MaxFeatures is how many feature flags fit into a caller's set.
const MaxFeatures = 256

// Features is a set of feature flags: bit i is flag Schema.Features[i]. It
// is a fixed-size value passed in a call without allocation.
type Features [MaxFeatures / 64]uint64

// Has reports whether flag i is on.
func (f *Features) Has(i int) bool {
	return i >= 0 && i < MaxFeatures && f[i/64]&(1<<(i%64)) != 0
}

// Set turns flag i on.
func (f *Features) Set(i int) {
	if i >= 0 && i < MaxFeatures {
		f[i/64] |= 1 << (i % 64)
	}
}

// Or adds the flags of g to the set.
func (f *Features) Or(g Features) {
	for i := range f {
		f[i] |= g[i]
	}
}

// Empty reports that no flag is on.
func (f *Features) Empty() bool { return *f == Features{} }

// CommandCall is a command call. ID, Args and the caller's Subject lie in
// arena Arena, which the engine owns: the interpreter copies them into the
// arena of the unit of work, so a retried run can reuse the same call.
type CommandCall struct {
	Entity  TypeRef
	ID      Value
	Command MethodRef
	Args    Record
	Arena   *Arena
	Caller  Caller
	Now     time.Time // the engine fixes it; it does not change on a retry
	// Contexts are the bounded contexts of the engine process
	// (docs/plans/0011, B6): ACL reactions of other contexts do not go into
	// the unit of work; the receiver's process runs them from the event
	// stream. nil means all bounded contexts.
	Contexts []string
	// Event: the call is an event rather than a command: Command is an event
	// of entity Entity with identifier ID and arguments Args that another
	// process has already committed; the reactions to it of the ACLs of
	// Contexts run.
	Event bool
}

// QueryCall is a call of an entity or projection query.
type QueryCall struct {
	Type   TypeRef
	Query  MethodRef
	Args   Record
	Arena  *Arena
	Caller Caller
	Now    time.Time
}

// Env is the interpreter's access to data; every engine implements it (RFC
// §30.4). The engine writes records and identifiers into arena a of the
// current unit of work and reads the arguments (ids, condition values) from it.
type Env interface {
	// Load returns an entity or projection record. If there is none, it
	// returns defaults, exists == false, version == 0. Personal fields of a
	// forgotten subject come back as None.
	Load(ctx context.Context, a *Arena, t TypeRef, id Value) (state Record, version uint64, exists bool, err error)
	// Find runs an index search. The compiler has already checked the
	// specification.
	Find(ctx context.Context, a *Arena, q *FindSpec) (Rows, error)
	// NewID issues an identifier by the type's strategy (uuid, sequence);
	// t == 0 is a timer identifier.
	NewID(ctx context.Context, a *Arena, t TypeRef) (Value, error)
}

// CondOp is a search condition operator.
type CondOp uint8

const (
	OpEq CondOp = iota
	OpNe
	OpLt
	OpLe
	OpGt
	OpGe
	OpIn // Value is a list or a set
)

// Cond is a search condition on a field.
type Cond struct {
	Field FieldPath
	Op    CondOp
	Value Value
}

// NoIndex is a search without an index (a full scan; only in migrations and
// where the compiler allowed it).
const NoIndex = ^IndexRef(0)

// FindSpec is an index search: either a page (Page) or Limit.
type FindSpec struct {
	Type   TypeRef
	Index  IndexRef // NoIndex: without an index
	Where  []Cond   // a conjunction of conditions on the index fields
	Sort   []SortKey
	Page   bool
	Size   int   // page size
	Cursor Value // page cursor: string or None
	Limit  int   // without Page; < 0 means no limit (all in migrations)
}

// Rows are the found records.
type Rows struct {
	// Records is a list of records in the arena. A row record carries its
	// identifier as the last value: the fields per the schema, then the id.
	Records Value
	Next    Value // next page cursor: string; None at the end
}
