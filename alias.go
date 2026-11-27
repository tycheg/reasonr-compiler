package reasonr

// The boundary types (RFC §30): the values, calls and results the model
// exchanges with the engine, the callbacks it calls (Env, Driver) and the
// test results. They are declared in the compiler's internal packages and
// exported here by aliases.

import (
	time "time"

	model "github.com/tycheg/reasonr-compiler/internal/model"
	bytecode "github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
	dec "github.com/tycheg/reasonr-compiler/internal/runtime/dec"
	jsonv "github.com/tycheg/reasonr-compiler/internal/runtime/jsonv"
	record "github.com/tycheg/reasonr-compiler/internal/runtime/record"
	vm "github.com/tycheg/reasonr-compiler/internal/runtime/vm"
)

// From github.com/tycheg/reasonr-compiler/reasonr.

// API is the external API contract of a model (RFC §31.3.1): published
// methods, deprecations and snapshots of supported versions. Outside test
// mode the engine exposes only published methods in /api and serves earlier
// versions with translations from snapshots and was.
type API = model.API

// APIDeprecation is a deprecated method: it may be removed after Sunset.
type APIDeprecation = model.APIDeprecation

// APIVersion is a snapshot of the published API on a release date
// (api/versions/<date>.json): the methods and the types they reach.
type APIVersion = model.APIVersion

// Access is an access condition.
type Access = model.Access

// AccessKind is the kind of an access condition.
type AccessKind = model.AccessKind

// Arena is the memory of a unit of work: the bytes of strings and bytes,
// the field values of records and the elements of collections. Values
// refer to it through ArenaRef. Buffers grow and are reused after Reset: in
// steady state there are no allocations.
//
// Composite values are built bottom-up: first the elements (strings, nested
// records), then the record itself — Push puts elements on the arena stack,
// End moves them into the arena in one piece. The slices Elems and Fields
// return are valid until the next allocation in the arena; values in the
// arena do not change.
//
// A reference with the high bit of Off set points into the program's static
// arena (constants): a regular arena reads it through Static. An arena is
// not meant for concurrent use from several goroutines.
type Arena = model.Arena

// ArenaRef is an offset and a length in an arena: in bytes for strings and
// bytes, in values for records and collections. The high bit of Off marks
// the program's static arena (constants).
type ArenaRef = model.ArenaRef

// ArgSubject is the personal data subject in argument Arg (the parameter
// number). An argument with data of several subjects has several records
// with one Arg.
type ArgSubject = model.ArgSubject

// BusinessError is a business error: a declared one (error …) or a built-in
// one (forbidden, invalidArgument, a uniqueness error from IndexInfo.Error).
type BusinessError = model.BusinessError

// Caller is an authenticated caller. The interpreter checks access (RFC
// §24.3): for CallerUser calls at the start of a public command or query; a
// violation is the forbidden error.
type Caller = model.Caller

// CallerKind is who calls a command or a query.
type CallerKind = model.CallerKind

// CommandCall is a command call. ID, Args and the caller's Subject lie in
// arena Arena, which the engine owns: the interpreter copies them into the
// arena of the unit of work, so a retried run can reuse the same call.
type CommandCall = model.CommandCall

// Cond is a search condition on a field.
type Cond = model.Cond

// CondOp is a search condition operator.
type CondOp = model.CondOp

// EntityChange is the changes of one entity.
type EntityChange = model.EntityChange

// Env is the interpreter's access to data; every engine implements it (RFC
// §30.4). The engine writes records and identifiers into arena a of the
// current unit of work and reads the arguments (ids, condition values) from it.
type Env = model.Env

// Event is an entity event.
type Event = model.Event

// ExternalRequest is a request to an external system.
type ExternalRequest = model.ExternalRequest

// FakeCall is a call of a public model command by the fake of system System.
type FakeCall = model.FakeCall

// FeatureInfo is a feature flag: the full name ("Ordering.tips"), the date
// it was introduced YYYY-MM-DD and the description from "##".
type FeatureInfo = model.FeatureInfo

// Features is a set of feature flags: bit i is flag Schema.Features[i]. It
// is a fixed-size value passed in a call without allocation.
type Features = model.Features

// FieldInfo is a field or a parameter.
type FieldInfo = model.FieldInfo

// FieldPath is a path to a field, including into a value object (car.plate).
type FieldPath = model.FieldPath

type FieldRef = model.FieldRef

// FieldValue is the value of one field (a projection row record).
type FieldValue = model.FieldValue

// FindSpec is an index search: either a page (Page) or Limit.
type FindSpec = model.FindSpec

// IDStrategy is how an identifier is issued.
type IDStrategy = model.IDStrategy

// IndexInfo is an index.
type IndexInfo = model.IndexInfo

type IndexRef = model.IndexRef

// Interpreter runs a compiled program: the base model or a tenant model
// (RFC §30.2). An error is a runtime failure: the engine answers
// internalError. Results refer to the arena of the unit of work and are
// valid until Release.
type Interpreter = model.Interpreter

// Kind is a value kind; it matches the language types.
type Kind = model.Kind

// MethodInfo is a method of a type.
type MethodInfo = model.MethodInfo

// MethodKind is the kind of a method.
type MethodKind = model.MethodKind

type MethodRef = model.MethodRef

// Notification is a notification.
type Notification = model.Notification

// Outcome is the result of a command: errors or a unit of work (RFC §30.2).
// The values lie in arena Arena; the engine reads them and calls Release,
// after which they are invalid. Results from NewOutcome come from a pool
// together with their slices: in steady state they do not allocate.
type Outcome = model.Outcome

// Personal is the personal data subject of a field or parameter.
type Personal = model.Personal

// ProjectionOp is an operation on a projection row.
type ProjectionOp = model.ProjectionOp

// ProjectionOpKind is the kind of a projection operation.
type ProjectionOpKind = model.ProjectionOpKind

// QueryCall is a call of an entity or projection query.
type QueryCall = model.QueryCall

// QueryOutcome is the result of a query: errors or a value.
type QueryOutcome = model.QueryOutcome

// Record is a record (a value object, an entity or projection state, the
// arguments): field values in schema order lie in the arena.
type Record = model.Record

// Rows are the found records.
type Rows = model.Rows

// Schema is a model schema: the base one or one with an extension.
type Schema = model.Schema

// SortKey is an index or sort key.
type SortKey = model.SortKey

// Subject is a personal data subject.
type Subject = model.Subject

// TimerOp schedules or deletes a timer.
type TimerOp = model.TimerOp

// TimerOpKind is the kind of a timer operation.
type TimerOpKind = model.TimerOpKind

// Type is the type of a field, a parameter or a result. Optionality is a
// property of the type: collection elements have it too (list<string?>).
type Type = model.Type

// TypeInfo is a declaring type.
type TypeInfo = model.TypeInfo

// TypeKind is the kind of a declaring type.
type TypeKind = model.TypeKind

type TypeRef = model.TypeRef

// UI is the UI program of a model (RFC §22; RFC §22):
// apps, screens, primitives and texts. The compiler builds it next to the
// schema, but it is not part of it (decision 2): storage, compatibility and
// migrations do not concern the UI. From the program the engine assembles
// screen descriptions for platform runtimes and calls the methods of places.
//
// Types are written as language strings ("Order.Id", "page<OrderRow>"); the
// type descriptions are in the model schema. Screen nodes are numbered from
// 1 in traversal order: arguments refer to data, row and form nodes by number.
type UI = model.UI

// UIApp is an app.
type UIApp = model.UIApp

// UIArg is a method argument, a command target or a screen parameter and its source.
type UIArg = model.UIArg

// UIBind is a binding to a command or a query.
type UIBind = model.UIBind

// UIExpr is an expression the server evaluates (decision 4): a bytecode
// function of kind ui with parameters Args, or a constant Value.
type UIExpr = model.UIExpr

// UINode is a layout line.
type UINode = model.UINode

// UIParam is a parameter of a screen, an event or a query parameter of a primitive.
type UIParam = model.UIParam

// UIPlace is a place: a node bound to a command or a query (RFC §22.10).
type UIPlace = model.UIPlace

// UIPrimValue is the value of a primitive parameter.
type UIPrimValue = model.UIPrimValue

// UIScreen is a screen.
type UIScreen = model.UIScreen

// UnitOfWork is the effects of a unit of work (RFC §30.3).
type UnitOfWork = model.UnitOfWork

// Value is a value without boxing (RFC-0001, §30.1). Scalars lie in Lo and
// Hi; strings, bytes, records and collections lie in the arena (Ref):
//
//	int, bigint          Lo: a two's complement integer (32 bits for int)
//	float                Lo: math.Float64bits
//	decimal              Lo, Hi: dec.Dec.Bits, an integer divided by 10¹⁸
//	bool                 Lo: 0 or 1
//	uuid, Timer.Id       Hi: bytes 0–7, Lo: bytes 8–15 (big-endian)
//	date                 Lo: days since 0001-01-01
//	time                 Lo: nanoseconds since midnight
//	timestamp            Lo: signed Unix seconds, Hi: nanoseconds 0–999 999 999
//	duration             Lo: signed nanoseconds
//	enum                 Type: the enum, Lo: the value number
//	string, bytes        Ref: the bytes in the arena
//	record               Type: the type, Ref: the fields in schema order
//	list, set            Ref: the elements; for a set in insertion order
//	map                  Ref: keys and values in turn, in insertion order
//	page                 Ref: two values, items (list) and next (string or none)
//	pageRequest          Ref: two values, size (int) and cursor (string or none)
//	<Entity>.Id          Type: the entity or projection; a uuid in Hi/Lo, a sequence in Lo,
//	                     string and composite: one value in the arena (Ref.Len == 1)
//
// A value that refers to an arena is valid while the arena lives.
type Value = model.Value

const AccessActor = model.AccessActor

const AccessAny = model.AccessAny

const AccessField = model.AccessField

const AccessRole = model.AccessRole

const AccessSelf = model.AccessSelf

const CallerFake = model.CallerFake

const CallerMigration = model.CallerMigration

const CallerTest = model.CallerTest

const CallerTimer = model.CallerTimer

const CallerUser = model.CallerUser

const IDDerived = model.IDDerived

const IDGiven = model.IDGiven

const IDNone = model.IDNone

const IDSequence = model.IDSequence

const IDUUID = model.IDUUID

const KindBigint = model.KindBigint

const KindBool = model.KindBool

const KindBytes = model.KindBytes

const KindDate = model.KindDate

const KindDecimal = model.KindDecimal

const KindDuration = model.KindDuration

const KindEnum = model.KindEnum

const KindFloat = model.KindFloat

const KindID = model.KindID

const KindInt = model.KindInt

const KindList = model.KindList

const KindMap = model.KindMap

const KindNone = model.KindNone

const KindPage = model.KindPage

const KindPageRequest = model.KindPageRequest

const KindRecord = model.KindRecord

const KindSet = model.KindSet

const KindString = model.KindString

const KindTime = model.KindTime

const KindTimerID = model.KindTimerID

const KindTimestamp = model.KindTimestamp

const KindUUID = model.KindUUID

const MethodCommand = model.MethodCommand

const MethodError = model.MethodError

const MethodEvent = model.MethodEvent

const MethodNotification = model.MethodNotification

const MethodQuery = model.MethodQuery

const MethodRequest = model.MethodRequest

const MethodRule = model.MethodRule

const MethodValue = model.MethodValue

// NoIndex is a search without an index (a full scan; only in migrations and
// where the compiler allowed it).
const NoIndex = model.NoIndex

const OpEq = model.OpEq

const OpGe = model.OpGe

const OpGt = model.OpGt

const OpIn = model.OpIn

const OpLe = model.OpLe

const OpLt = model.OpLt

const OpNe = model.OpNe

const ProjectionDelete = model.ProjectionDelete

const ProjectionUpsert = model.ProjectionUpsert

const TimerDelete = model.TimerDelete

const TimerSchedule = model.TimerSchedule

const TypeActor = model.TypeActor

const TypeEntity = model.TypeEntity

const TypeEnum = model.TypeEnum

const TypeExternal = model.TypeExternal

const TypeProjection = model.TypeProjection

const TypeValue = model.TypeValue

const UIFromData = model.UIFromData

const UIFromEvent = model.UIFromEvent

const UIFromExpr = model.UIFromExpr

const UIFromForm = model.UIFromForm

const UIFromInput = model.UIFromInput

const UIFromMe = model.UIFromMe

const UIFromNew = model.UIFromNew

const UIFromParam = model.UIFromParam

const UIFromPrimitive = model.UIFromPrimitive

const UIFromProvided = model.UIFromProvided

const UIFromRow = model.UIFromRow

// None is the absence of a value of T?.
var None = model.None

// AppendValue appends value v with all nested data of arena a to dst.
func AppendValue(dst []byte, a *Arena, v Value) []byte { return model.AppendValue(dst, a, v) }

// Bigint returns a bigint value.
func Bigint(x int64) Value { return model.Bigint(x) }

// Bool returns a bool value.
func Bool(b bool) Value { return model.Bool(b) }

// Compare orders value x from arena ax and value y from arena ay.
func Compare(ax *Arena, x Value, ay *Arena, y Value) int { return model.Compare(ax, x, ay, y) }

// DateDays returns a date by the number of days since 0001-01-01.
func DateDays(days int64) Value { return model.DateDays(days) }

// Decimal returns a decimal value.
func Decimal(d Dec) Value { return model.Decimal(d) }

// Duration returns a duration.
func Duration(d time.Duration) Value { return model.Duration(d) }

// Enum returns value number i of enum t.
func Enum(t TypeRef, i int) Value { return model.Enum(t, i) }

// Equal compares value x from arena ax with value y from arena ay.
func Equal(ax *Arena, x Value, ay *Arena, y Value) bool { return model.Equal(ax, x, ay, y) }

// FakeID returns the identifier of the single fake instance of external
// system t.
func FakeID(t TypeRef) Value { return model.FakeID(t) }

// Float returns a float value.
func Float(x float64) Value { return model.Float(x) }

// GetArena takes an empty arena from the pool.
func GetArena() *Arena { return model.GetArena() }

// ID returns an identifier of entity or projection t over a uuid or a
// sequence (inner is KindUUID or KindBigint). Arena.ID creates string and
// composite identifiers.
func ID(t TypeRef, inner Value) Value { return model.ID(t, inner) }

// Int returns an int value (the caller is responsible for the 32-bit range).
func Int(x int64) Value { return model.Int(x) }

// NewArena returns an empty arena.
func NewArena() *Arena { return model.NewArena() }

// NewOutcome takes an empty result with arena a from the pool.
func NewOutcome(a *Arena) *Outcome { return model.NewOutcome(a) }

// NewQueryOutcome takes an empty query result with arena a from the pool.
func NewQueryOutcome(a *Arena) *QueryOutcome { return model.NewQueryOutcome(a) }

// ParseSchema reads a schema from JSON and checks the type numbers.
func ParseSchema(data []byte) (*Schema, error) { return model.ParseSchema(data) }

// PutArena returns an arena to the pool; its values become invalid.
func PutArena(a *Arena) { model.PutArena(a) }

// ReadValue reads a value from the start of src into arena a and returns the rest.
func ReadValue(src []byte, a *Arena) (Value, []byte, error) { return model.ReadValue(src, a) }

// TimeOfDay returns a time of day: d since midnight, 0 ≤ d < 24h.
func TimeOfDay(d time.Duration) Value { return model.TimeOfDay(d) }

// TimerID returns a timer identifier.
func TimerID(u [16]byte) Value { return model.TimerID(u) }

// Timestamp returns an instant.
func Timestamp(t time.Time) Value { return model.Timestamp(t) }

// UUID returns a uuid value.
func UUID(u [16]byte) Value { return model.UUID(u) }

// From github.com/tycheg/reasonr-compiler/internal/runtime/dec.

// Dec is a decimal number. The zero value is 0.
type Dec = dec.Dec

// DecFromInt returns an integer number; it never overflows.
func DecFromInt(x int64) Dec { return dec.FromInt(x) }

// From github.com/tycheg/reasonr-compiler/internal/runtime/vm.

// Driver is the engine a test runs on (RFC §27.3): memory calls the engine
// directly, http goes through the API. Step values lie in its arena and are
// valid until the next driver call.
type Driver = vm.Driver

// Failure is a test failure. Values are in language form, identifiers by
// the names of test variables.
type Failure = vm.Failure

// FailureKind is the kind of a test failure.
type FailureKind = vm.FailureKind

// FakeError is the errors of a model command a fake called (RFC §18,
// decision 11).
type FakeError = vm.FakeError

// Fault is a runtime failure: the engine answers internalError, developers
// get the place of the failure and the model call stack.
type Fault = vm.Fault

// FeatureDriver is a driver that turns on a test's feature flags (the
// feature line at the start of a test, RFC §23.4, decision 8): a flag
// is turned on for the whole system before the first step. In a test
// without a feature line all flags are off.
type FeatureDriver = vm.FeatureDriver

// Phase is the part of a test a run executes.
type Phase = vm.Phase

// Section is a test section.
type Section = vm.Section

// SectionDriver is a driver that cares about the section of a step: UI
// drivers run given through the test API, and when and then through UI
// places (RFC §27.3). Section is called before every step.
type SectionDriver = vm.SectionDriver

// Step is the outcome of a step: errors, delivered notifications (To is the
// recipient) and external requests, the query result, the errors of
// commands external system fakes called in the step's cascade.
type Step = vm.Step

// StepLog is a test step and its outcome (reasonrd test -v).
type StepLog = vm.StepLog

// TestOptions are the settings of a test run.
type TestOptions = vm.TestOptions

// TestResult is the outcome of a test.
type TestResult = vm.TestResult

const FailFake = vm.FailFake

const FailFault = vm.FailFault

const FailMismatch = vm.FailMismatch

const FailMissingError = vm.FailMissingError

const FailMissingNotification = vm.FailMissingNotification

const FailMissingRequest = vm.FailMissingRequest

const FailNoResult = vm.FailNoResult

const FailStep = vm.FailStep

const FailUI = vm.FailUI

const FailUnexpectedError = vm.FailUnexpectedError

const Given = vm.Given

const PhaseAll = vm.PhaseAll

const PhaseGiven = vm.PhaseGiven

const PhaseWhen = vm.PhaseWhen

const Then = vm.Then

const When = vm.When

var ErrCheck = vm.ErrCheck

var ErrConvert = vm.ErrConvert

var ErrDivZero = vm.ErrDivZero

var ErrInvalidRegex = vm.ErrInvalidRegex

var ErrLimit = vm.ErrLimit

var ErrNotFound = vm.ErrNotFound

var ErrOverflow = vm.ErrOverflow

// From github.com/tycheg/reasonr-compiler/internal/runtime/bytecode.

// IDField is the id "field" of an entity or projection record in search
// paths, upsert and matches.
const IDField = bytecode.IDField

// From github.com/tycheg/reasonr-compiler/internal/runtime/jsonv.

// JSONCodec encodes and decodes the values of a schema. It is safe for
// concurrent use.
type JSONCodec = jsonv.Codec

// JSONError is an encoding or decoding error: Path is the path to the place
// ("route.from.lat", "items[2]"), "" for the root; Rule is the violated
// rule in invalidArgument terms (RFC §14): type, required, range, encoding,
// unknown.
type JSONError = jsonv.Error

// AppendJSONString appends string s in quotes by the JSON rules.
func AppendJSONString(dst []byte, s string) []byte { return jsonv.AppendString(dst, s) }

// NewJSONCodec returns a codec of schema s.
func NewJSONCodec(s *Schema) *JSONCodec { return jsonv.New(s) }

// From github.com/tycheg/reasonr-compiler/internal/runtime/record.

// RecordCodec encodes and decodes values per a schema. It is safe for concurrent
// use: between calls it keeps only the cache of default records.
type RecordCodec = record.Codec

// NewRecordCodec returns a codec of schema s.
func NewRecordCodec(s *Schema) *RecordCodec { return record.New(s) }
