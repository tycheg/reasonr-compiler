// Package types describes ReasonR types and the names a model declares:
// declaration types (entities, projections, value objects…) and their
// members. Package sema builds them.
package types

import (
	"unicode"
	"unicode/utf8"

	"github.com/tycheg/reasonr-compiler/internal/ast"
)

// Type is the type of a value.
type Type interface {
	String() string
	typ()
}

// BasicKind is the kind of a primitive type.
type BasicKind uint8

const (
	Int BasicKind = iota
	Bigint
	Float
	Decimal
	String
	Bool
	UUID
	Date
	Time
	Timestamp
	Duration
	Bytes
)

var basicNames = [...]string{
	Int: "int", Bigint: "bigint", Float: "float", Decimal: "decimal", String: "string", Bool: "bool",
	UUID: "uuid", Date: "date", Time: "time", Timestamp: "timestamp", Duration: "duration", Bytes: "bytes",
}

// Basics are the primitive types by name.
var Basics = func() map[string]*Basic {
	m := map[string]*Basic{}
	for k, name := range basicNames {
		m[name] = &Basic{Kind: BasicKind(k)}
	}
	return m
}()

type (
	// Basic is a primitive type.
	Basic struct{ Kind BasicKind }
	// Optional — T?.
	Optional struct{ Elem Type }
	// List — list<T>.
	List struct{ Elem Type }
	// Set — set<T>.
	Set struct{ Elem Type }
	// Map — map<K, V>.
	Map struct{ Key, Elem Type }
	// Page — page<T>.
	Page struct{ Elem Type }
	// Enum is an enum.
	Enum struct{ Decl *Decl }
	// Value is a value object.
	Value struct{ Decl *Decl }
	// ID is the identifier type of an entity, actor or projection: Order.Id.
	// Base is uuid, bigint (sequence), string, a value object or an entity
	// ID (for a projection "id Order.Id").
	ID struct {
		Owner *Decl
		Base  Type
	}
	// TimerID — Timer.Id.
	TimerID struct{}
	// Ref is [x] in commands: a reference to an entity without field access.
	Ref struct{ Owner *Decl }
	// Record is a record with fields: [x] in queries of its own type, after.
	Record struct{ Owner *Decl }
	// Caller — caller.
	Caller struct{}
	// PageReq — PageRequest.
	PageReq struct{}
	// Void is the result of commands, events and other methods without a result.
	Void struct{}
	// Invalid is the type after an error; compatible with everything, it suppresses cascades.
	Invalid struct{}
	// None is the type of the none literal until the expected optional type is known.
	None struct{}
	// TypeVar is a type variable of a UI primitive (<R, K id>) or a
	// constraint in default (id, enum, value): filled in at the use site.
	TypeVar struct {
		Name       string
		Constraint string // "", "id", "enum", "value"
	}
	// Union is A | B: one of the types; only in primitive declarations.
	Union struct{ Alts []Type }
)

func (*Basic) typ()    {}
func (*Optional) typ() {}
func (*List) typ()     {}
func (*Set) typ()      {}
func (*Map) typ()      {}
func (*Page) typ()     {}
func (*Enum) typ()     {}
func (*Value) typ()    {}
func (*ID) typ()       {}
func (*TimerID) typ()  {}
func (*Ref) typ()      {}
func (*Record) typ()   {}
func (*Caller) typ()   {}
func (*PageReq) typ()  {}
func (*Void) typ()     {}
func (*Invalid) typ()  {}
func (*None) typ()     {}
func (*TypeVar) typ()  {}
func (*Union) typ()    {}

func (t *Basic) String() string    { return basicNames[t.Kind] }
func (t *Optional) String() string { return t.Elem.String() + "?" }
func (t *List) String() string     { return "list<" + t.Elem.String() + ">" }
func (t *Set) String() string      { return "set<" + t.Elem.String() + ">" }
func (t *Map) String() string      { return "map<" + t.Key.String() + ", " + t.Elem.String() + ">" }
func (t *Page) String() string     { return "page<" + t.Elem.String() + ">" }
func (t *Enum) String() string     { return t.Decl.Name }
func (t *Value) String() string    { return t.Decl.Name }
func (t *ID) String() string       { return t.Owner.Name + ".Id" }
func (*TimerID) String() string    { return "Timer.Id" }
func (t *Ref) String() string      { return "[" + t.Owner.Name + "]" }
func (t *Record) String() string   { return t.Owner.Name }
func (*Caller) String() string     { return "Caller" }
func (*PageReq) String() string    { return "PageRequest" }
func (*Void) String() string       { return "void" }
func (*Invalid) String() string    { return "invalid" }
func (*None) String() string       { return "none" }
func (t *TypeVar) String() string  { return t.Name }
func (t *Union) String() string {
	s := ""
	for i, a := range t.Alts {
		if i > 0 {
			s += " | "
		}
		s += a.String()
	}
	return s
}

// Unwrap returns T for T? and the type itself otherwise.
func Unwrap(t Type) Type {
	if o, ok := t.(*Optional); ok {
		return o.Elem
	}
	return t
}

// IsInvalid reports whether the type is the result of an error.
func IsInvalid(t Type) bool {
	_, ok := t.(*Invalid)
	return ok
}

// ---------------------------------------------------------------------------
// Declarations

// DeclKind is the kind of a top-level declaration.
type DeclKind uint8

const (
	EntityDecl DeclKind = iota
	ActorDecl
	ProjectionDecl
	ValueDecl
	EnumDecl
	ExternalDecl
	AppDecl
	ScreenDecl
	PrimitiveDecl
)

var declKindNames = [...]string{
	EntityDecl: "entity", ActorDecl: "actor", ProjectionDecl: "projection", ValueDecl: "value",
	EnumDecl: "enum", ExternalDecl: "external", AppDecl: "app", ScreenDecl: "screen", PrimitiveDecl: "primitive",
}

func (k DeclKind) String() string { return declKindNames[k] }

// IsUI reports whether the kind belongs to the UI: an app, a screen, a primitive.
func (k DeclKind) IsUI() bool { return k >= AppDecl }

// HasID reports whether declarations of this kind have an identifier.
func (k DeclKind) HasID() bool { return k == EntityDecl || k == ActorDecl || k == ProjectionDecl }

// Decl is a named top-level declaration and its members.
type Decl struct {
	Kind DeclKind
	// Name is the full name (docs/plans/0011, §3.6): "Ordering.Money",
	// "Ordering.Order"; for a flat module, apps and primitives it is the
	// name as written.
	Name string
	// Short is the name as written: "Money", "Order".
	Short string
	// Context is the bounded context of the declaration: "Ordering"; "" outside contexts.
	Context string
	Node    ast.Decl
	// Owner is the aggregate of an inner entity (entity Order.Stop,
	// docs/plans/0011, §3.5); Inner lists the inner entities of an aggregate.
	Owner *Decl
	Inner []*Decl
	// Callers, for an actor, are the actors of other bounded contexts that
	// act as this actor with the same identifier through an ACL
	// (actor Auth.User as Customer).
	Callers []*Decl
	// ID is the identifier type (entities, actors, projections).
	ID *ID
	// Role is the role on whose behalf an external system answers
	// (external(role)); "" when not set.
	Role string
	// Fake: the external system has a fake (a test program): fields, events,
	// commands and request implementations; ID is its single instance.
	Fake bool

	Fields  []*Var // fields of an entity, projection or value object in declaration order
	Indexes []*Index
	Methods []*Method // commands, events, notifications, errors, queries, rules, value methods, external system requests
	Values  []*EnumValue
	Params  []*Var // screen parameters

	fields  map[string]*Var
	methods map[string]*Method
	values  map[string]*EnumValue
}

// InnerNamed returns an inner entity of an aggregate by its short name, or nil.
func (d *Decl) InnerNamed(name string) *Decl {
	for _, x := range d.Inner {
		if x.Short == name {
			return x
		}
	}
	return nil
}

// Root returns the aggregate of a declaration: the aggregate for an inner
// entity, the declaration itself otherwise.
func (d *Decl) Root() *Decl {
	if d.Owner != nil {
		return d.Owner
	}
	return d
}

// Field returns a field by name, or nil.
func (d *Decl) Field(name string) *Var { return d.fields[name] }

// Method returns a method by name, or nil.
func (d *Decl) Method(name string) *Method { return d.methods[name] }

// Value returns an enum value by name, or nil.
func (d *Decl) Value(name string) *EnumValue { return d.values[name] }

// AddField adds a field; the caller rejects duplicates.
func (d *Decl) AddField(v *Var) {
	if d.fields == nil {
		d.fields = map[string]*Var{}
	}
	d.Fields = append(d.Fields, v)
	d.fields[v.Name] = v
}

// AddMethod adds a method; the caller rejects duplicates.
func (d *Decl) AddMethod(m *Method) {
	if d.methods == nil {
		d.methods = map[string]*Method{}
	}
	d.Methods = append(d.Methods, m)
	d.methods[m.Name] = m
}

// AddValue adds an enum value; the caller rejects duplicates.
func (d *Decl) AddValue(v *EnumValue) {
	if d.values == nil {
		d.values = map[string]*EnumValue{}
	}
	d.Values = append(d.Values, v)
	d.values[v.Name] = v
}

// Var is a field or a parameter.
type Var struct {
	Name     string
	Node     ast.Node // *ast.FieldDecl or *ast.Param
	Type     Type
	Personal bool
}

// Index is an index of an entity or a projection.
type Index struct {
	Name string
	Node *ast.IndexDecl
}

// EnumValue is an enum value.
type EnumValue struct {
	Name  string
	Node  *ast.EnumValue
	Index int
}

// MethodKind is the kind of a method.
type MethodKind uint8

const (
	Command MethodKind = iota
	Event
	Notification
	Error
	Query
	Rule
	ValueMethod // a value object method
	Request     // a request to an external system
)

var methodKindNames = [...]string{
	Command: "command", Event: "event", Notification: "notification", Error: "error",
	Query: "query", Rule: "rule", ValueMethod: "method", Request: "request",
}

func (k MethodKind) String() string { return methodKindNames[k] }

// Method is a method of a declaration.
type Method struct {
	Kind   MethodKind
	Name   string
	Node   ast.Member
	Params []*Var
	Result Type // Void when there is no result
}

// Public reports whether the method is public (commands and queries starting with a capital letter).
func (m *Method) Public() bool {
	return (m.Kind == Command || m.Kind == Query) && IsUpper(m.Name)
}

// IsUpper reports whether a name starts with a capital letter.
func IsUpper(name string) bool {
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}
