package model

import (
	"sync"
	"time"
)

// Outcome is the result of a command: errors or a unit of work (RFC §30.2).
// The values lie in arena Arena; the engine reads them and calls Release,
// after which they are invalid. Results from NewOutcome come from a pool
// together with their slices: in steady state they do not allocate.
type Outcome struct {
	Errors []BusinessError // non-empty → Work == nil
	Work   *UnitOfWork
	Arena  *Arena

	pooled bool
	work   UnitOfWork // storage of Work
	errs   []BusinessError
	events []Event
	sets   []FieldValue
}

var outcomes = sync.Pool{New: func() any { return &Outcome{} }}

// NewOutcome takes an empty result with arena a from the pool.
func NewOutcome(a *Arena) *Outcome {
	o := outcomes.Get().(*Outcome)
	o.pooled, o.Arena = true, a
	return o
}

// SetErrors records the errors (a copy of errs) and drops the unit of work.
func (o *Outcome) SetErrors(errs []BusinessError) {
	o.errs = append(o.errs[:0], errs...)
	o.Errors, o.Work = o.errs, nil
}

// NewWork returns an empty unit of work of the result: its slices reuse the
// memory of earlier results from the pool.
func (o *Outcome) NewWork() *UnitOfWork {
	w := &o.work
	w.Entities, w.Projections, w.Timers = w.Entities[:0], w.Projections[:0], w.Timers[:0]
	w.Notifications, w.Requests, w.Forget = w.Notifications[:0], w.Requests[:0], w.Forget[:0]
	w.Calls = w.Calls[:0]
	o.events, o.sets = o.events[:0], o.sets[:0]
	o.Errors, o.Work = nil, w
	return w
}

// Events returns a copy of the events in the result's storage.
func (o *Outcome) Events(evs []Event) []Event {
	n := len(o.events)
	o.events = append(o.events, evs...)
	return o.events[n:len(o.events):len(o.events)]
}

// FieldValues returns a copy of the field values in the result's storage.
func (o *Outcome) FieldValues(fs []FieldValue) []FieldValue {
	n := len(o.sets)
	o.sets = append(o.sets, fs...)
	return o.sets[n:len(o.sets):len(o.sets)]
}

// Release returns the arena of the unit of work and the result itself to
// the pool. A repeated call does nothing.
func (o *Outcome) Release() {
	if o.Arena == nil && o.Work == nil && o.Errors == nil {
		return
	}
	PutArena(o.Arena)
	o.Arena, o.Work, o.Errors = nil, nil, nil
	if o.pooled {
		o.pooled = false
		outcomes.Put(o)
	}
}

// QueryOutcome is the result of a query: errors or a value.
type QueryOutcome struct {
	Errors []BusinessError // non-empty → Result.Kind == KindNone
	Result Value           // a value object, page or list
	Arena  *Arena

	pooled bool
	errs   []BusinessError
}

var queryOutcomes = sync.Pool{New: func() any { return &QueryOutcome{} }}

// NewQueryOutcome takes an empty query result with arena a from the pool.
func NewQueryOutcome(a *Arena) *QueryOutcome {
	o := queryOutcomes.Get().(*QueryOutcome)
	o.pooled, o.Arena, o.Result = true, a, None
	return o
}

// SetErrors records the errors (a copy of errs) and drops the value.
func (o *QueryOutcome) SetErrors(errs []BusinessError) {
	o.errs = append(o.errs[:0], errs...)
	o.Errors, o.Result = o.errs, None
}

// Release returns the query arena and the result itself to the pool. A
// repeated call does nothing.
func (o *QueryOutcome) Release() {
	if o.Arena == nil && o.Errors == nil {
		return
	}
	PutArena(o.Arena)
	o.Arena, o.Errors, o.Result = nil, nil, None
	if o.pooled {
		o.pooled = false
		queryOutcomes.Put(o)
	}
}

// BusinessError is a business error: a declared one (error …) or a built-in
// one (forbidden, invalidArgument, a uniqueness error from IndexInfo.Error).
type BusinessError struct {
	Owner TypeRef // the entity or projection that declared the error; 0 for a built-in
	Name  string
	Args  Record
	// Subjects are the personal data subjects in the arguments: the engine
	// encrypts the response by retry key with them (RFC §20).
	Subjects []ArgSubject
}

// UnitOfWork is the effects of a unit of work (RFC §30.3).
type UnitOfWork struct {
	Entities      []EntityChange
	Projections   []ProjectionOp
	Timers        []TimerOp
	Notifications []Notification
	Requests      []ExternalRequest
	Forget        []Subject // crypto-shredding: destroy the keys after commit
	// Calls are calls of public model commands by an external system fake
	// (the test program): the engine runs them after commit from the system
	// role, like the answers of a real system through the API.
	Calls []FakeCall
}

// FakeCall is a call of a public model command by the fake of system System.
type FakeCall struct {
	System  TypeRef
	Entity  TypeRef
	ID      Value
	Command MethodRef
	Args    Record
}

// FakeID returns the identifier of the single fake instance of external
// system t.
func FakeID(t TypeRef) Value { return ID(t, UUID([16]byte{})) }

// EntityChange is the changes of one entity.
type EntityChange struct {
	Type    TypeRef
	ID      Value
	Existed bool    // whether the entity existed when loaded
	Version uint64  // the version when loaded (0 if it did not exist)
	Events  []Event // in order
	State   Record  // the state after the events
}

// Event is an entity event.
type Event struct {
	Name MethodRef
	Args Record
	Seq  uint32 // the order of the event among all events of the unit of work
	// Subjects are the subjects of personal arguments: the store fills them
	// when reading the outbox (consumers must encrypt or hide them and do not
	// have the entity state that personal paths are computed from).
	Subjects []ArgSubject
}

// ProjectionOpKind is the kind of a projection operation.
type ProjectionOpKind uint8

const (
	ProjectionUpsert ProjectionOpKind = iota
	ProjectionDelete
)

// ProjectionOp is an operation on a projection row.
type ProjectionOp struct {
	Kind       ProjectionOpKind
	Projection TypeRef
	ID         Value
	Set        []FieldValue // the passed fields (for Upsert); the rest get defaults
	Seq        uint32       // the order of operations on one row matters
}

// TimerOpKind is the kind of a timer operation.
type TimerOpKind uint8

const (
	TimerSchedule TimerOpKind = iota
	TimerDelete
)

// TimerOp schedules or deletes a timer.
type TimerOp struct {
	Kind     TimerOpKind
	ID       Value     // Timer.Id
	At       time.Time // for Schedule
	Entity   TypeRef   // the entity and the command for Schedule
	EntityID Value
	Command  MethodRef
	Args     Record
	// Subjects are the subjects of personal arguments: the store encrypts
	// the timer arguments with them (RFC §20).
	Subjects []ArgSubject
}

// Notification is a notification.
type Notification struct {
	Owner TypeRef
	Name  MethodRef
	Args  Record
	To    Value // the actor id; None: to the initiator of the root command
	// Subjects are the personal data subjects in the arguments.
	Subjects []ArgSubject
}

// ExternalRequest is a request to an external system.
type ExternalRequest struct {
	System  TypeRef
	Request MethodRef
	// Args are the arguments, the deadline timer included (MethodInfo.Timeout).
	Args Record
	Key  Value // a unique request id (for idempotency at the receiver)
	// Subjects are the personal data subjects in the arguments: the engine
	// encrypts the request in the outbox with them.
	Subjects []ArgSubject
}

// Subject is a personal data subject.
type Subject struct {
	Type TypeRef
	ID   Value
}

// ArgSubject is the personal data subject in argument Arg (the parameter
// number). An argument with data of several subjects has several records
// with one Arg.
type ArgSubject struct {
	Arg     int
	Subject Subject
}
