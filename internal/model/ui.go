package model

import "encoding/json"

// UI is the UI program of a model (RFC §22; RFC §22):
// apps, screens, primitives and texts. The compiler builds it next to the
// schema, but it is not part of it (decision 2): storage, compatibility and
// migrations do not concern the UI. From the program the engine assembles
// screen descriptions for platform runtimes and calls the methods of places.
//
// Types are written as language strings ("Order.Id", "page<OrderRow>"); the
// type descriptions are in the model schema. Screen nodes are numbered from
// 1 in traversal order: arguments refer to data, row and form nodes by number.
type UI struct {
	Apps       []UIApp       `json:"apps"`
	Screens    []UIScreen    `json:"screens"`
	Primitives []UIPrimitive `json:"primitives"`
	// Types are the model's enums and value objects: the labels of values
	// and fields in screen descriptions.
	Types []UIType `json:"types"`
	// Texts are texts by language and key (RFC §22.11): layout strings in the
	// app's base language and texts blocks.
	Texts map[string]map[string]string `json:"texts,omitempty"`
}

// UIType is an enum (Values) or a value object (Fields).
type UIType struct {
	Name   string    `json:"name"`
	Values []string  `json:"values,omitempty"`
	Fields []UIParam `json:"fields,omitempty"`
}

// Type returns a type by name, or nil.
func (u *UI) Type(name string) *UIType {
	for i := range u.Types {
		if u.Types[i].Name == name {
			return &u.Types[i]
		}
	}
	return nil
}

// UIApp is an app.
type UIApp struct {
	Name      string    `json:"name"`
	Title     string    `json:"title,omitempty"`
	Language  string    `json:"language"`
	Platforms []string  `json:"platforms"`
	Roles     []string  `json:"roles,omitempty"`  // the roles that enter
	Actors    []string  `json:"actors,omitempty"` // the actor types that enter
	Me        string    `json:"me,omitempty"`     // the actor type of me
	Auto      bool      `json:"auto,omitempty"`   // the automatic UI built the layout
	Nodes     []UINode  `json:"nodes,omitempty"`  // the root: usually a menu
	Screens   []string  `json:"screens"`          // the screens that can be reached
	Places    []UIPlace `json:"places,omitempty"`
}

// UIPlace is a place: a node bound to a command or a query (RFC §22.10).
type UIPlace struct {
	Name    string `json:"name"`             // Admin.AdminOrder:Order.Edit
	Screen  string `json:"screen,omitempty"` // "" is the app root
	Node    int    `json:"node"`
	Type    string `json:"type"`
	Method  string `json:"method"`
	Command bool   `json:"command,omitempty"`
}

// UIScreen is a screen.
type UIScreen struct {
	Name   string    `json:"name"`
	Title  string    `json:"title,omitempty"`
	Params []UIParam `json:"params,omitempty"`
	Nodes  []UINode  `json:"nodes"`
}

// UIParam is a parameter of a screen, an event or a query parameter of a primitive.
type UIParam struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Node kinds of UINode.Kind.
const (
	UIPrimitiveNode = "primitive"
	UIPartNode      = "part"
	UIScreenNode    = "screen" // a nested screen or a navigation item
	UIFieldNode     = "field"
	UIOnNode        = "on"
)

// UINode is a layout line.
type UINode struct {
	ID        int    `json:"id"`
	Kind      string `json:"kind"`
	Primitive string `json:"primitive,omitempty"`
	Part      string `json:"part,omitempty"`
	// Field is the field name; FieldType is its type; Input: the field is input.
	Field     string `json:"field,omitempty"`
	FieldType string `json:"fieldType,omitempty"`
	Input     bool   `json:"input,omitempty"`
	// Key is the node key in texts and places without a screen: Order.Edit, Toolbar#2.
	Key string `json:"key,omitempty"`
	// Text is the model text key that labels the field: OrderRow.placedAt
	// or Customer.OrderTaxi.requestedClass (RFC §22.11).
	Text  string `json:"text,omitempty"`
	Label string `json:"label,omitempty"` // the layout string in the base language
	// Auto: the automatic UI built the node (an omitted part or field).
	Auto bool `json:"auto,omitempty"`
	// Rows: the lines repeat in every row of the node's query result: for a
	// part its lines, for a primitive the children outside parts.
	Rows bool `json:"rows,omitempty"`
	// Types are the inferred type variables of the primitive.
	Types  map[string]string `json:"types,omitempty"`
	Params []UIPrimValue     `json:"params,omitempty"`
	Bind   *UIBind           `json:"bind,omitempty"`
	To     *UITransition     `json:"to,omitempty"`
	Event  string            `json:"event,omitempty"` // UIOnNode
	Back   bool              `json:"back,omitempty"`
	If     *UIExpr           `json:"if,omitempty"`
	// Children are the children and parts; the compiler has already filled
	// in omitted parts that bind data (Auto).
	Children []UINode `json:"children,omitempty"`
}

// UIBind is a binding to a command or a query.
type UIBind struct {
	Type    string  `json:"type"`
	Method  string  `json:"method"`
	Command bool    `json:"command,omitempty"`
	Result  string  `json:"result,omitempty"` // the query result type
	Target  *UIArg  `json:"target,omitempty"` // the command target
	Args    []UIArg `json:"args,omitempty"`
}

// UITransition is a transition to a screen: Link, a menu item, an embedding, on.
type UITransition struct {
	Screen string  `json:"screen"`
	Args   []UIArg `json:"args,omitempty"`
}

// Where the value of UIArg.From comes from (RFC §22.4).
const (
	UIFromExpr      = "expr"      // an expression: Expr
	UIFromParam     = "param"     // screen parameter Field
	UIFromMe        = "me"        // the caller
	UIFromNew       = "new"       // a new identifier
	UIFromData      = "data"      // field Field of the data of node Node
	UIFromRow       = "row"       // field Field of a row of node Node
	UIFromForm      = "form"      // value Field of the form of node Node
	UIFromEvent     = "event"     // the event data of node Node
	UIFromProvided  = "provided"  // a value the primitive passes (text for search)
	UIFromPrimitive = "primitive" // the primitive fills it: PageRequest
	UIFromInput     = "input"     // the user enters it
)

// UIArg is a method argument, a command target or a screen parameter and its source.
type UIArg struct {
	Name  string  `json:"name"`
	Type  string  `json:"type"`
	From  string  `json:"from"`
	Node  int     `json:"node,omitempty"`
	Field string  `json:"field,omitempty"`
	Expr  *UIExpr `json:"expr,omitempty"`
	// Input: the user enters the value; if From is not input, the source
	// gives the initial input value.
	Input bool `json:"input,omitempty"`
	// Default: the input has the parameter's default value; Value is the
	// value itself in canonical JSON if it is a constant.
	Default bool            `json:"default,omitempty"`
	Value   json.RawMessage `json:"value,omitempty"`
}

// UIExpr is an expression the server evaluates (decision 4): a bytecode
// function of kind ui with parameters Args, or a constant Value.
type UIExpr struct {
	Func  string          `json:"func,omitempty"`
	Args  []UIArg         `json:"args,omitempty"`
	Value json.RawMessage `json:"value,omitempty"` // canonical JSON (RFC §31.3)
}

// UIPrimValue is the value of a primitive parameter.
type UIPrimValue struct {
	Name   string  `json:"name"`
	Value  *UIExpr `json:"value,omitempty"`  // a simple parameter
	Query  *UIBind `json:"query,omitempty"`  // a query parameter
	Field  string  `json:"field,omitempty"`  // field<R[, K]>
	Screen string  `json:"screen,omitempty"` // screen
}

// UIPrimitive is a primitive declaration: platform implementation
// interfaces are generated from it (reasonr build --ui-types).
type UIPrimitive struct {
	Name     string        `json:"name"`
	Std      bool          `json:"std,omitempty"` // the standard catalog
	Vars     []UITypeVar   `json:"vars,omitempty"`
	Binds    string        `json:"binds,omitempty"` // "", query, command, screen, field, input
	BindType string        `json:"bindType,omitempty"`
	Params   []UIPrimParam `json:"params,omitempty"`
	Children *UIPart       `json:"children,omitempty"`
	Parts    []UIPart      `json:"parts,omitempty"`
	Events   []UIEvent     `json:"events,omitempty"`
}

// UITypeVar is a type variable of a primitive.
type UITypeVar struct {
	Name       string `json:"name"`
	Constraint string `json:"constraint,omitempty"`
}

// UIPrimParam is a primitive parameter.
type UIPrimParam struct {
	Name      string    `json:"name"`
	Kind      string    `json:"kind"` // value, query, search, field, screen
	Type      string    `json:"type,omitempty"`
	Provided  []UIParam `json:"provided,omitempty"`
	Record    string    `json:"record,omitempty"`
	FieldType string    `json:"fieldType,omitempty"`
	Optional  bool      `json:"optional,omitempty"`
	Auto      bool      `json:"auto,omitempty"`
}

// UIPart is a part of a primitive or its children.
type UIPart struct {
	Name  string `json:"name,omitempty"`
	Binds string `json:"binds,omitempty"` // "", type, params, inputs, screens
	Type  string `json:"type,omitempty"`
}

// UIEvent is a primitive event.
type UIEvent struct {
	Name   string    `json:"name"`
	Params []UIParam `json:"params,omitempty"`
}

// App returns an app by name, or nil.
func (u *UI) App(name string) *UIApp {
	for i := range u.Apps {
		if u.Apps[i].Name == name {
			return &u.Apps[i]
		}
	}
	return nil
}

// Screen returns a screen by name, or nil: by full name (Ordering.MyOrder)
// or by short name (MyOrder) if such a screen is unique.
func (u *UI) Screen(name string) *UIScreen {
	var found *UIScreen
	for i := range u.Screens {
		s := &u.Screens[i]
		switch {
		case s.Name == name:
			return s
		case shortName(s.Name) == name:
			if found != nil {
				return nil
			}
			found = s
		}
	}
	return found
}

// Primitive returns a primitive by name, or nil.
func (u *UI) Primitive(name string) *UIPrimitive {
	for i := range u.Primitives {
		if u.Primitives[i].Name == name {
			return &u.Primitives[i]
		}
	}
	return nil
}

// Node returns a screen node by number, or nil.
func (s *UIScreen) Node(id int) *UINode { return findUINode(s.Nodes, id) }

func findUINode(nodes []UINode, id int) *UINode {
	for i := range nodes {
		if nodes[i].ID == id {
			return &nodes[i]
		}
		if n := findUINode(nodes[i].Children, id); n != nil {
			return n
		}
	}
	return nil
}
