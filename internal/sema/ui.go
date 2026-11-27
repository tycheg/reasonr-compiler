package sema

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/constant"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// The model UI (RFC §22; RFC §22): apps, screens,
// primitives, default primitives and texts, the result of checking the
// layout. The UI program is built from it (stage U2); the LSP, explain and
// the UI test runner work from it.

// UI is the model UI.
type UI struct {
	Apps       []*App
	Screens    []*Screen
	Primitives []*Primitive // the standard catalog, then the module's primitives
	// Defaults are the top-level default primitives: the module's, then the catalog's.
	Defaults []*Default
	// Texts are texts by language and key.
	Texts map[string]map[string]*Text
	// Nodes are the resolved layout lines.
	Nodes map[ast.UINode]*Node

	apps    map[string]*App
	screens map[string]*Screen
	// find looks up a screen by name from the place it is written (namespaces, scope.go).
	find  func(at ast.Node, name string) *Screen
	prims map[string]*Primitive
}

// App returns an app by name or nil.
func (u *UI) App(name string) *App { return u.apps[name] }

// Screen returns a screen by name or nil.
func (u *UI) Screen(name string) *Screen { return u.screens[name] }

// Primitive returns a primitive by name or nil.
func (u *UI) Primitive(name string) *Primitive { return u.prims[name] }

// App is an app: who enters, the base language, platforms, the menu and screens.
type App struct {
	Name      string
	Decl      *types.Decl
	Node      *ast.AppDecl
	Title     string
	Language  string   // the base language of layout strings; en by default
	Platforms []string // web by default
	Auto      bool
	Roles     []string      // the roles that enter
	Actors    []*types.Decl // the actor types that enter
	// Me is the app's actor type (the identifier for me) or nil.
	Me       *types.Decl
	Defaults []*Default
	Nodes    []*Node   // the root layout
	Screens  []*Screen // the screens that can be reached, in traversal order
	Places   []*Place
	// AutoSource is the layout the automatic UI built (app … auto): reasonr
	// ui eject prints it.
	AutoSource []byte
}

// Screen is a screen: parameters, a title, a layout.
type Screen struct {
	Name   string
	Decl   *types.Decl
	Node   *ast.ScreenDecl
	Params []*types.Var
	Title  string
	Nodes  []*Node
	// Apps are the apps from which the screen can be reached.
	Apps []*App
	// Me is the type of me on the screen: the actor of all its apps, or nil.
	Me *types.Decl
}

// NodeKind is what a layout line turned out to be.
type NodeKind uint8

const (
	NodePrimitive NodeKind = iota // a primitive
	NodePart                      // a part of the parent primitive
	NodeScreen                    // a nested screen or a navigation item
	NodeField                     // a data field or an input
	NodeOn                        // a transition on an event
)

// Role is displaying a value or an input.
type Role uint8

const (
	RoleShow Role = iota
	RoleInput
)

// Node is a resolved layout line.
type Node struct {
	Kind   NodeKind
	AST    ast.UINode
	Screen *Screen // nil for the app root layout
	Parent *Node

	Prim  *Primitive
	Subst map[string]types.Type // the inferred type variables of the primitive
	// Params are the values of the primitive's parameters by name.
	Params map[string]*PrimValue
	// Default: the field's primitive comes from default.
	Default *Default

	Part *Part // NodePart

	// The binding to a method: the type, the command or query, the arguments and the command target.
	Owner  *types.Decl
	Method *types.Method
	Args   []*Arg
	Target *Arg // the command target; nil for queries
	// To is the screen of a binding (Link), an embedding, a navigation item or a transition.
	To     *Screen
	ToArgs []*Arg

	// A field: name, type and role; TextKey is the model text key that labels
	// the field: OrderRow.placedAt or Customer.OrderTaxi.requestedClass.
	Field     string
	FieldType types.Type
	Role      Role
	TextKey   string

	Event *PrimEvent // NodeOn; nil for back
	Back  bool

	Label string
	Cond  *Expr // the visibility condition or nil
	// Auto lists the parts and children the automatic UI fills: part names,
	// "children" for children.
	Auto []string
	// Key is the node key in texts without a screen: Order.Edit, Table#2, status.
	Key string

	Children []*Node

	s subst // the inferred type variables
}

// Bound reports whether the node is bound to a command or a query: such a node is a place.
func (n *Node) Bound() bool { return n.Method != nil }

// SourceKind is where the value of an argument, a target or a screen parameter comes from.
type SourceKind uint8

const (
	SrcExpr      SourceKind = iota // an expression in the binding
	SrcParam                       // a screen parameter
	SrcMe                          // me
	SrcNew                         // new: a new identifier
	SrcData                        // a field of the primitive's query data
	SrcRow                         // a field of a table or list row
	SrcForm                        // a value of the command form
	SrcEvent                       // the event data
	SrcProvided                    // a value the primitive passes (text for search)
	SrcPrimitive                   // the primitive fills it: PageRequest
	SrcInput                       // an input: a query filter or a form field
)

var sourceNames = [...]string{
	SrcExpr: "expression", SrcParam: "screen parameter", SrcMe: "me", SrcNew: "new", SrcData: "data",
	SrcRow: "row", SrcForm: "form", SrcEvent: "event", SrcProvided: "primitive value",
	SrcPrimitive: "primitive", SrcInput: "input",
}

func (k SourceKind) String() string { return sourceNames[k] }

// Fixed reports whether the value is fixed: it is not in the form (RFC §22.4).
func (k SourceKind) Fixed() bool {
	switch k {
	case SrcExpr, SrcParam, SrcMe, SrcNew, SrcProvided, SrcPrimitive:
		return true
	}
	return false
}

// Arg is a method argument or a screen parameter and its source.
type Arg struct {
	Name   string
	Type   types.Type
	Source SourceKind
	From   *Node    // the data, row or form node; nil for the rest
	Field  string   // the name of the value in the source
	Expr   ast.Expr // as written in the binding: SrcExpr, SrcNew, an explicit name
	Code   *Expr    // SrcExpr: the expression the server evaluates
	// Input: the user enters the value: SrcInput, or a command argument from
	// data, a row, a form or an event, and then Source gives the initial
	// input value (RFC §22.4, item 3).
	Input bool
	// Default: the input has the parameter's default value.
	Default bool
	// Param is the method or screen parameter; nil for a command target.
	Param *types.Var
}

// Expr is a layout expression the server evaluates (RFC §22,
// decision 4): an if condition, an expression in an argument or a primitive
// parameter. Uses are the scope values it reads, in order of first
// occurrence: these are the parameters of its bytecode function (Vars). A
// constant needs no function: its value is Const.
type Expr struct {
	X       ast.Expr
	Type    types.Type
	Uses    []*Arg
	Vars    []*types.Var
	Const   constant.Value
	IsConst bool
	// Func is the name of the bytecode function: ui.<screen or app>:<node key>.<what>.
	Func string
}

// Place is a place: a layout node bound to a command or a query (RFC
// §22.10): Admin.AdminOrder:Order.Edit, the second such node is #2.
type Place struct {
	Name   string
	App    *App
	Screen *Screen // nil for the app root
	Node   *Node
}

// Methods are the commands and queries the UI binds to: layout nodes, query
// parameters of primitives and default.
func (u *UI) Methods() []*types.Method {
	seen := map[*types.Method]bool{}
	var out []*types.Method
	add := func(m *types.Method) {
		if m != nil && !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	params := func(vals map[string]*PrimValue) {
		for _, v := range vals {
			add(v.Method)
		}
	}
	var walk func([]*Node)
	walk = func(nodes []*Node) {
		for _, n := range nodes {
			add(n.Method)
			params(n.Params)
			walk(n.Children)
		}
	}
	for _, a := range u.Apps {
		walk(a.Nodes)
		for _, d := range a.Defaults {
			params(d.Params)
		}
	}
	for _, s := range u.Screens {
		walk(s.Nodes)
	}
	for _, d := range u.Defaults {
		params(d.Params)
	}
	return out
}

// LineName is a name that can be written as a field line under a node.
type LineName struct {
	Name string
	Type types.Type
}

// LineNames is what lowercase lines under a node mean: value fields, query
// filters or command inputs (RFC §22.7).
func (n *Node) LineNames() []LineName {
	pt, owner := (*Part)(nil), n
	switch {
	case n.Kind == NodePart:
		pt, owner = n.Part, n.Parent
	case n.Prim != nil:
		pt = n.Prim.Children
	}
	if pt == nil || owner == nil {
		return nil
	}
	var out []LineName
	switch pt.Binds {
	case PartType:
		if v, ok := apply(pt.Type, owner.subst()).(*types.Value); ok {
			for _, f := range v.Decl.Fields {
				out = append(out, LineName{Name: f.Name, Type: f.Type})
			}
		}
	case PartParams, PartInputs:
		for _, a := range owner.allArgs() {
			if a.Input {
				out = append(out, LineName{Name: a.Name, Type: a.Type})
			}
		}
	}
	return out
}
