// Package reasonr holds the types of the "compiler ↔ interpreter ↔ engine"
// boundary (RFC-0001, §30): the model schema (§30.5), which the compiler
// builds and from which the engine creates storage, the API, encryption and
// the UI; values and the arena of a unit of work (§30.1); calls, results and
// the unit of work (§30.2, §30.3); the Interpreter and Env interfaces (§30.4).
package model

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// TypeRef, FieldRef, MethodRef, IndexRef are schema numbers. The numbers are
// internal: stored data refers to names, so the numbers may change between
// model versions.
type (
	TypeRef   uint32 // 0: no type (built-in, not an actor)
	FieldRef  uint16
	MethodRef uint32
	IndexRef  uint16
)

// FieldPath is a path to a field, including into a value object (car.plate).
type FieldPath []FieldRef

// Kind is a value kind; it matches the language types.
type Kind uint8

const (
	KindNone   Kind = iota // none of an optional
	KindInt                // int: 32 bits
	KindBigint             // bigint: 64 bits
	KindFloat
	KindDecimal
	KindString
	KindBool
	KindUUID
	KindDate
	KindTime
	KindTimestamp
	KindDuration
	KindBytes
	KindEnum    // the value number
	KindID      // an entity identifier; inside is a uuid, bigint, string or record
	KindTimerID // a timer identifier
	KindRecord  // a value object, an entity or projection record
	KindList
	KindSet
	KindMap
	KindPage        // page<T>: a query result
	KindPageRequest // PageRequest: a query parameter
)

var kindNames = []string{
	"none", "int", "bigint", "float", "decimal", "string", "bool", "uuid", "date", "time", "timestamp", "duration", "bytes",
	"enum", "id", "timerId", "record", "list", "set", "map", "page", "pageRequest",
}

// Schema is a model schema: the base one or one with an extension.
type Schema struct {
	// API is the external API contract (RFC §31.3.1); nil when there is no api/ directory.
	API       *API       `json:"api,omitempty"`
	Version   uint64     `json:"version,omitempty"`   // the engine assigns it
	Extension string     `json:"extension,omitempty"` // "" is the base model
	Types     []TypeInfo `json:"types"`               // by kind, then by name; Ref = index + 1
	Roles     []string   `json:"roles,omitempty"`     // by name
	// Features are the model's feature flags by full name: the number in the
	// list is the flag's bit in the caller's set (Caller.Features). The
	// engine, not the schema, keeps the flag state (for whom they are on,
	// RFC §23.4).
	Features []FeatureInfo `json:"features,omitempty"`
	// Migrations are the names of the module's manual migrations: the
	// version compatibility check considers whether there is a new migration.
	Migrations []string `json:"migrations,omitempty"`
}

// FeatureInfo is a feature flag: the full name ("Ordering.tips"), the date
// it was introduced YYYY-MM-DD and the description from "##".
type FeatureInfo struct {
	Name string `json:"name"`
	Date string `json:"date"`
	Doc  string `json:"doc,omitempty"`
}

// Feature returns the number of a flag by full name, or -1.
func (s *Schema) Feature(name string) int {
	for i := range s.Features {
		if s.Features[i].Name == name {
			return i
		}
	}
	return -1
}

// API is the external API contract of a model (RFC §31.3.1): published
// methods, deprecations and snapshots of supported versions. Outside test
// mode the engine exposes only published methods in /api and serves earlier
// versions with translations from snapshots and was.
type API struct {
	Methods    []string         `json:"methods,omitempty"` // "Ordering.Order.Cancel", alphabetically
	Deprecated []APIDeprecation `json:"deprecated,omitempty"`
	Versions   []APIVersion     `json:"versions,omitempty"` // by date
}

// APIDeprecation is a deprecated method: it may be removed after Sunset.
type APIDeprecation struct {
	Method string `json:"method"`
	Sunset string `json:"sunset"` // YYYY-MM-DD
}

// APIVersion is a snapshot of the published API on a release date
// (api/versions/<date>.json): the methods and the types they reach.
type APIVersion struct {
	Date    string      `json:"date"`
	Methods []APIMethod `json:"methods"`
	Types   []APIType   `json:"types,omitempty"`
}

// APIMethod is a published method of a version.
type APIMethod struct {
	Type   string     `json:"type"` // "Ordering.Order"
	Name   string     `json:"name"`
	Kind   string     `json:"kind"` // command or query
	Create bool       `json:"create,omitempty"`
	Params []APIField `json:"params,omitempty"`
	Result string     `json:"result,omitempty"` // a type: "Ordering.OrderCard", "list<…>"
}

// APIField is a method parameter or a value object field of a version.
type APIField struct {
	Name    string `json:"name"`
	Type    string `json:"type"` // a type; "?" marks an optional
	Default bool   `json:"default,omitempty"`
}

// APIType is a value object or an enum of a version.
type APIType struct {
	Name   string     `json:"name"`
	Fields []APIField `json:"fields,omitempty"`
	Values []string   `json:"values,omitempty"`
}

// FullName returns "Type.method".
func (m *APIMethod) FullName() string { return m.Type + "." + m.Name }

// Version returns the snapshot of version date, or nil.
func (a *API) Version(date string) *APIVersion {
	if a == nil {
		return nil
	}
	for i := range a.Versions {
		if a.Versions[i].Date == date {
			return &a.Versions[i]
		}
	}
	return nil
}

// Method returns version method "Type.name", or nil.
func (v *APIVersion) Method(typ, name string) *APIMethod {
	for i := range v.Methods {
		if v.Methods[i].Type == typ && v.Methods[i].Name == name {
			return &v.Methods[i]
		}
	}
	return nil
}

// Type returns a version type by name, or nil.
func (v *APIVersion) Type(name string) *APIType {
	for i := range v.Types {
		if v.Types[i].Name == name {
			return &v.Types[i]
		}
	}
	return nil
}

// Published reports whether method "Type.name" is published.
func (a *API) Published(full string) bool {
	if a == nil {
		return false
	}
	i := sort.SearchStrings(a.Methods, full)
	return i < len(a.Methods) && a.Methods[i] == full
}

// Deprecation returns the deprecation of method "Type.name", or nil.
func (a *API) Deprecation(full string) *APIDeprecation {
	if a == nil {
		return nil
	}
	for i := range a.Deprecated {
		if a.Deprecated[i].Method == full {
			return &a.Deprecated[i]
		}
	}
	return nil
}

// TypeText returns the text of a schema type for version snapshots:
// "string", "Ordering.Money", "list<Ordering.OrderRow>", "Ordering.Order.Id?".
func (s *Schema) TypeText(t Type) string {
	var out string
	switch t.Kind {
	case KindID:
		if ti := s.Type(t.Ref); ti != nil {
			out = ti.Name + ".Id"
		}
	case KindRecord, KindEnum:
		if ti := s.Type(t.Ref); ti != nil {
			out = ti.Name
		}
	case KindList, KindSet, KindPage:
		name := map[Kind]string{KindList: "list", KindSet: "set", KindPage: "page"}[t.Kind]
		if t.Elem != nil {
			out = name + "<" + s.TypeText(*t.Elem) + ">"
		}
	case KindMap:
		if t.Key != nil && t.Elem != nil {
			out = "map<" + s.TypeText(*t.Key) + ", " + s.TypeText(*t.Elem) + ">"
		}
	default:
		out = t.Kind.String()
	}
	if t.Optional {
		out += "?"
	}
	return out
}

// TypeKind is the kind of a declaring type.
type TypeKind uint8

const (
	TypeEntity TypeKind = iota
	TypeActor
	TypeProjection
	TypeValue
	TypeEnum
	TypeExternal
)

var typeKindNames = []string{"entity", "actor", "projection", "value", "enum", "external"}

// IDStrategy is how an identifier is issued.
type IDStrategy uint8

const (
	IDNone     IDStrategy = iota // not an entity
	IDUUID                       // uuid
	IDSequence                   // sequence
	IDGiven                      // string or composite: the caller sets it
	IDDerived                    // based on an entity identifier (a projection with id Order.Id)
)

var idStrategyNames = []string{"none", "uuid", "sequence", "given", "derived"}

// TypeInfo is a declaring type.
type TypeInfo struct {
	Ref     TypeRef      `json:"ref"`
	Name    string       `json:"name"`
	Kind    TypeKind     `json:"kind"`
	ID      IDStrategy   `json:"id,omitempty"`
	IDType  TypeRef      `json:"idType,omitempty"` // a composite identifier or the base type for IDDerived
	Fields  []FieldInfo  `json:"fields,omitempty"` // in declaration order; extension fields last
	Indexes []IndexInfo  `json:"indexes,omitempty"`
	Methods []MethodInfo `json:"methods,omitempty"` // by kind, then by name
	Enum    []string     `json:"enum,omitempty"`    // enum values in order
	// EnumWas holds the old names of enum values (rub was rubles,
	// RFC §31.3.1) in Enum order; "" means not renamed; nil when none.
	EnumWas []string `json:"enumWas,omitempty"`
	// Extended: the type is declared or changed by an extension.
	Extended bool `json:"extended,omitempty"`
	// Role is the role on whose behalf an external system answers
	// (external(role)); "" when not set.
	Role string `json:"role,omitempty"`
	// Owner is the aggregate of an inner entity (entity Order.Stop,
	// docs/plans/0011, §3.5). An inner entity is stored in the aggregate
	// document, in its Inner field of the same name (Stop): a map id → state
	// record; its events are written as aggregate events "Stop.arrived" with
	// the id as the first argument.
	Owner TypeRef `json:"owner,omitempty"`
	// Callers, for an actor, are the actors of other bounded contexts that
	// act as this actor with the same identifier through an ACL (actor
	// Auth.User as Customer, docs/plans/0011): caller Auth.User:u passes
	// Customer access, and me is Customer:u.
	Callers []TypeRef `json:"callers,omitempty"`
}

// ActsAs reports whether calling actor caller acts as actor t: it is t
// itself or an actor from t.Callers.
func (t *TypeInfo) ActsAs(caller TypeRef) bool {
	if caller == 0 {
		return false
	}
	if caller == t.Ref {
		return true
	}
	for _, c := range t.Callers {
		if c == caller {
			return true
		}
	}
	return false
}

// Type is the type of a field, a parameter or a result. Optionality is a
// property of the type: collection elements have it too (list<string?>).
type Type struct {
	Kind     Kind    `json:"kind"`
	Ref      TypeRef `json:"ref,omitempty"`  // Enum, ID (the owner), Record (a value object, entity, projection)
	Elem     *Type   `json:"elem,omitempty"` // List, Set, Page, Map (the value)
	Key      *Type   `json:"key,omitempty"`  // Map
	Optional bool    `json:"optional,omitempty"`
}

// FieldInfo is a field or a parameter.
type FieldInfo struct {
	Ref       FieldRef  `json:"ref"`
	Name      string    `json:"name"`
	Type      Type      `json:"type"`
	Default   *Literal  `json:"default,omitempty"`   // nil: no default value
	Personal  *Personal `json:"personal,omitempty"`  // nil: a regular field
	WasName   string    `json:"was,omitempty"`       // for renames
	Extension string    `json:"extension,omitempty"` // "" for a base field, otherwise an extension field
	// Inner is an aggregate field with the records of the inner entity of
	// this type (a map id → state); 0 for a regular field.
	Inner TypeRef `json:"inner,omitempty"`
}

// Personal is the personal data subject of a field or parameter.
type Personal struct {
	Subject     FieldPath `json:"subject,omitempty"`     // empty: the entity itself
	SubjectType TypeRef   `json:"subjectType,omitempty"` // 0: any subject (an external request parameter)
}

// Literal is a schema constant: a default value. Text is the canonical form
// of a scalar: an integer, an exact decimal fraction, a floating-point
// number, a string, true/false, a duration in nanoseconds, a date, a time,
// an instant (RFC 3339), a uuid, an enum value name.
type Literal struct {
	Kind   Kind      `json:"kind"`
	Text   string    `json:"text,omitempty"`
	Fields []Literal `json:"fields,omitempty"` // Record: fields in schema order
	Elems  []Literal `json:"elems,omitempty"`  // List, Set; Map: keys and values in turn
}

// SortKey is an index or sort key.
type SortKey struct {
	Field FieldPath `json:"field"`
	Desc  bool      `json:"desc,omitempty"`
}

// IndexInfo is an index.
type IndexInfo struct {
	Ref    IndexRef  `json:"ref"`
	Name   string    `json:"name"`
	Keys   []SortKey `json:"keys"`
	Unique bool      `json:"unique,omitempty"`
	Error  string    `json:"error,omitempty"` // the error name on a uniqueness violation
}

// MethodKind is the kind of a method.
type MethodKind uint8

const (
	MethodCommand MethodKind = iota
	MethodEvent
	MethodNotification
	MethodError
	MethodQuery
	MethodRule
	MethodRequest // a request to an external system
	MethodValue   // a value object method
)

var methodKindNames = []string{"command", "event", "notification", "error", "query", "rule", "request", "method"}

// AccessKind is the kind of an access condition.
type AccessKind uint8

const (
	AccessAny   AccessKind = iota // any authenticated caller
	AccessSelf                    // the actor itself
	AccessField                   // the actor from an entity field
	AccessActor                   // any actor of the type
	AccessRole                    // a role
)

var accessKindNames = []string{"any", "self", "field", "actor", "role"}

// Access is an access condition.
type Access struct {
	Kind  AccessKind `json:"kind"`
	Field FieldPath  `json:"field,omitempty"` // for AccessField
	Actor TypeRef    `json:"actor,omitempty"` // for AccessActor
	Role  string     `json:"role,omitempty"`  // for AccessRole
}

// MethodInfo is a method of a type.
type MethodInfo struct {
	Ref    MethodRef   `json:"ref"`
	Name   string      `json:"name"`
	Kind   MethodKind  `json:"kind"`
	Public bool        `json:"public,omitempty"`
	Access []Access    `json:"access,omitempty"` // any of the conditions; none for private methods
	Params []FieldInfo `json:"params,omitempty"`
	Result *Type       `json:"result,omitempty"` // query, rule, value method
	// TimeoutParam is the number of the timeout parameter (Timer.Id) of a
	// request from 1: its timer is the delivery deadline (RFC §18); 0 means
	// the request has no deadline.
	TimeoutParam int `json:"timeoutParam,omitempty"`
	// Fake: the request is implemented by the system's fake (the test
	// program): the engine runs it as a command of the fake instance (FakeID).
	Fake      bool   `json:"fake,omitempty"`
	Extension string `json:"extension,omitempty"` // an extension method
	// Doc is the description of a public method from a ## comment (MCP, OpenAPI).
	Doc string `json:"doc,omitempty"`
	// Destructive: the command forgets a subject (forget) or deletes
	// (delete), so an agent asks the user (MCP, RFC §31.3.2).
	Destructive bool `json:"destructive,omitempty"`
	// Was is the old name of a public method (… was Cancel): clients of
	// earlier API versions call it by that name (RFC §31.3.1); "" when none.
	Was string `json:"was,omitempty"`
	// Create: a creation command (command(…) create Issue, docs/plans/0011,
	// §3.8), called without an identifier, which the engine issues by strategy.
	Create bool `json:"create,omitempty"`
	// ACL, for an event, lists by name the bounded contexts whose ACLs react
	// to it (docs/plans/0011, B6).
	ACL []string `json:"acl,omitempty"`
	// Inner, for an aggregate event "Stop.arrived", is the inner entity and
	// the event of it that it records; 0 for an own event.
	Inner      TypeRef   `json:"inner,omitempty"`
	InnerEvent MethodRef `json:"innerEvent,omitempty"`
}

// Timeout returns the deadline timer of an external request from its
// arguments (the TimeoutParam parameter); None means no deadline.
func (m *MethodInfo) Timeout(a *Arena, args Record) Value {
	if m.TimeoutParam == 0 || args.Value().Kind != KindRecord {
		return None
	}
	vals := a.Fields(args)
	if m.TimeoutParam > len(vals) {
		return None
	}
	return vals[m.TimeoutParam-1]
}

// Type returns a type by number, or nil.
func (s *Schema) Type(ref TypeRef) *TypeInfo {
	if ref == 0 || int(ref) > len(s.Types) {
		return nil
	}
	return &s.Types[ref-1]
}

// Lookup returns a type by name, or nil: by full name (Ordering.Money) or
// by short name (Money, Order) if it is unique in the schema.
func (s *Schema) Lookup(name string) *TypeInfo {
	for i := range s.Types {
		if s.Types[i].Name == name {
			return &s.Types[i]
		}
	}
	var found *TypeInfo
	for i := range s.Types {
		t := &s.Types[i]
		if shortName(t.Name) != name {
			continue
		}
		if found != nil {
			return nil // the short name is ambiguous
		}
		found = t
	}
	return found
}

// Method returns the type typeName names (Lookup) and its method name. nil
// when absent.
func (s *Schema) Method(typeName, name string) (*TypeInfo, *MethodInfo) {
	t := s.Lookup(typeName)
	if t == nil {
		return nil, nil
	}
	return t, t.Method(name)
}

// Role returns a schema role by its name from authentication. Roles are
// shared by the project (docs/plans/0011, §3.6); the full name, if any, is
// "Context.role" in schemas of earlier versions, the short one if the role
// is unique; otherwise the name as is.
func (s *Schema) Role(name string) string {
	found := ""
	for _, r := range s.Roles {
		switch {
		case r == name:
			return r
		case shortName(r) == name:
			if found != "" {
				return name
			}
			found = r
		}
	}
	if found != "" {
		return found
	}
	return name
}

// Short returns the short type name: Matching for Ordering.Matching. An
// integration's external system is known by its short name, unique in the
// project: request paths, integrations keys in engine.yaml, topics.
func (t *TypeInfo) Short() string { return shortName(t.Name) }

func shortName(name string) string {
	if i := strings.LastIndex(name, "."); i >= 0 {
		return name[i+1:]
	}
	return name
}

// Stored reports whether the engine stores records of the type: entities,
// actors, projections and external systems with a fake (the test program:
// a fake has the identifier of its single instance).
func (t *TypeInfo) Stored() bool {
	if t.Owner != 0 {
		return false // in the aggregate document
	}
	switch t.Kind {
	case TypeEntity, TypeActor, TypeProjection:
		return true
	case TypeExternal:
		return t.ID != IDNone
	}
	return false
}

// Field returns a field by name, or nil.
func (t *TypeInfo) Field(name string) *FieldInfo {
	for i := range t.Fields {
		if t.Fields[i].Name == name {
			return &t.Fields[i]
		}
	}
	return nil
}

// Method returns a method by name, or nil.
func (t *TypeInfo) Method(name string) *MethodInfo {
	for i := range t.Methods {
		if t.Methods[i].Name == name {
			return &t.Methods[i]
		}
	}
	return nil
}

// Index returns an index by name, or nil.
func (t *TypeInfo) Index(name string) *IndexInfo {
	for i := range t.Indexes {
		if t.Indexes[i].Name == name {
			return &t.Indexes[i]
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Kinds are written in JSON by name.

func (k Kind) String() string       { return name(kindNames, int(k)) }
func (k TypeKind) String() string   { return name(typeKindNames, int(k)) }
func (k IDStrategy) String() string { return name(idStrategyNames, int(k)) }
func (k MethodKind) String() string { return name(methodKindNames, int(k)) }
func (k AccessKind) String() string { return name(accessKindNames, int(k)) }

func name(names []string, i int) string {
	if i < len(names) {
		return names[i]
	}
	return fmt.Sprintf("?%d", i)
}

func parse(names []string, what string, text []byte) (int, error) {
	for i, n := range names {
		if n == string(text) {
			return i, nil
		}
	}
	return 0, fmt.Errorf("reasonr: unknown %s %q", what, text)
}

func (k Kind) MarshalText() ([]byte, error)       { return []byte(k.String()), nil }
func (k TypeKind) MarshalText() ([]byte, error)   { return []byte(k.String()), nil }
func (k IDStrategy) MarshalText() ([]byte, error) { return []byte(k.String()), nil }
func (k MethodKind) MarshalText() ([]byte, error) { return []byte(k.String()), nil }
func (k AccessKind) MarshalText() ([]byte, error) { return []byte(k.String()), nil }

func (k *Kind) UnmarshalText(b []byte) error {
	i, err := parse(kindNames, "value kind", b)
	*k = Kind(i)
	return err
}

func (k *TypeKind) UnmarshalText(b []byte) error {
	i, err := parse(typeKindNames, "type kind", b)
	*k = TypeKind(i)
	return err
}

func (k *IDStrategy) UnmarshalText(b []byte) error {
	i, err := parse(idStrategyNames, "identifier kind", b)
	*k = IDStrategy(i)
	return err
}

func (k *MethodKind) UnmarshalText(b []byte) error {
	i, err := parse(methodKindNames, "method kind", b)
	*k = MethodKind(i)
	return err
}

func (k *AccessKind) UnmarshalText(b []byte) error {
	i, err := parse(accessKindNames, "access kind", b)
	*k = AccessKind(i)
	return err
}

// MarshalIndent returns the schema as indented JSON (the schema file format).
func (s *Schema) MarshalIndent() ([]byte, error) {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// ParseSchema reads a schema from JSON and checks the type numbers.
func ParseSchema(data []byte) (*Schema, error) {
	var s Schema
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("reasonr: schema: %w", err)
	}
	for i, t := range s.Types {
		if t.Ref != TypeRef(i+1) {
			return nil, fmt.Errorf("reasonr: schema: type %s has number %d, expected %d", t.Name, t.Ref, i+1)
		}
	}
	return &s, nil
}
