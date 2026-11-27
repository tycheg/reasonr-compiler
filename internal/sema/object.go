package sema

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// ObjKind is what a name in an expression turned out to be.
type ObjKind uint8

const (
	ObjLocal     ObjKind = iota // let, a for variable
	ObjParam                    // a method parameter or an event argument in an on reaction
	ObjField                    // a field of the current entity, projection, value object or search record
	ObjFixture                  // a shared test let
	ObjBuiltin                  // id, now, today, after, hasErrors, me, caller
	ObjEnumValue                // an enum value
	ObjFeature                  // a system feature flag: Name is the full name
)

// Object is what a name refers to.
type Object struct {
	Kind  ObjKind
	Name  string
	Type  types.Type
	Node  ast.Node         // the declaration: a let/for name, *ast.Param, *ast.FieldDecl
	Var   *types.Var       // ObjParam, ObjField
	Value *types.EnumValue // ObjEnumValue
	Enum  *types.Decl      // ObjEnumValue
	// Feature is the flag declaration (ObjFeature).
	Feature *ast.FeatureDecl
}

// CallKind is the kind of a call.
type CallKind uint8

const (
	CallMethod        CallKind = iota // a method of its own type: a command, event, notification, error, rule, value method
	CallCommand                       // [x].command(...) of another (or its own) entity
	CallQuery                         // Type.query(...)
	CallRequest                       // System.request(...) to an external system
	CallConstruct                     // Type(...): a value object constructor
	CallUpsert                        // Projection(...): creating or updating a row
	CallConvert                       // Type.Id(x): an identifier conversion
	CallBuiltin                       // a built-in function: int(), round(), schedule()…
	CallBuiltinMethod                 // a built-in method: s.trim(), xs.with(x)…
	CallExternal                      // [x].Command(...) from a fake: a model API call from the system role
	CallFake                          // System.Command(...) in a test: a fake command
	CallEnumConvert                   // Enum(x): an enum value of another bounded context by name (ACL)
)

// Call is a resolved call.
type Call struct {
	Kind    CallKind
	Decl    *types.Decl   // the type that owns the method, or the constructed type
	Method  *types.Method // CallMethod, CallCommand, CallQuery, CallRequest, CallExternal
	Builtin string        // CallBuiltin, CallBuiltinMethod
}

var (
	tBool      = types.Basics["bool"]
	tInt       = types.Basics["int"]
	tBigint    = types.Basics["bigint"]
	tFloat     = types.Basics["float"]
	tDecimal   = types.Basics["decimal"]
	tString    = types.Basics["string"]
	tDate      = types.Basics["date"]
	tTimestamp = types.Basics["timestamp"]
	tDuration  = types.Basics["duration"]
	tNone      = &types.None{}
	tVoid      = &types.Void{}
)
