package ast

import "fmt"

// Visitor is called by Walk for every node. If Visit returns w != nil, Walk
// visits the node's children with visitor w and then calls w.Visit(nil).
type Visitor interface {
	Visit(node Node) (w Visitor)
}

// Walk traverses the tree depth-first in source order.
func Walk(v Visitor, node Node) {
	if v = v.Visit(node); v == nil {
		return
	}
	walkChildren(v, node)
	v.Visit(nil)
}

type inspector func(Node) bool

func (f inspector) Visit(n Node) Visitor {
	if f(n) {
		return f
	}
	return nil
}

// Inspect traverses the tree and calls f for every node; if f returns
// false, the node's children are skipped. f(nil) is called after the children.
func Inspect(node Node, f func(Node) bool) {
	Walk(inspector(f), node)
}

func walkIdent(v Visitor, x *Ident) {
	if x != nil {
		Walk(v, x)
	}
}

func walkExpr(v Visitor, x Expr) {
	if x != nil {
		Walk(v, x)
	}
}

func walkType(v Visitor, t TypeExpr) {
	if t != nil {
		Walk(v, t)
	}
}

func walkArgs(v Visitor, l *ArgList) {
	if l != nil {
		Walk(v, l)
	}
}

func walkParams(v Visitor, l *ParamList) {
	if l != nil {
		Walk(v, l)
	}
}

func walkAccess(v Visitor, l *AccessList) {
	if l != nil {
		Walk(v, l)
	}
}

func walkBlock(v Visitor, b *Block) {
	if b != nil {
		Walk(v, b)
	}
}

func walkDoc(v Visitor, g *CommentGroup) {
	if g != nil {
		Walk(v, g)
	}
}

func walkPersonal(v Visitor, p *Personal) {
	if p != nil {
		Walk(v, p)
	}
}

func walkChildren(v Visitor, node Node) {
	switch n := node.(type) {
	// Common parts
	case *File:
		for _, d := range n.Decls {
			Walk(v, d)
		}
	case *CommentGroup, *Ident, *BasicLit, *BadDecl, *BadMember, *BadStmt, *BadExpr, *BadType, *BranchStmt:
	case *Path:
		for _, e := range n.Elems {
			Walk(v, e)
		}
	case *AccessList:
		for _, x := range n.Names {
			Walk(v, x)
		}
	case *ParamList:
		for _, p := range n.List {
			Walk(v, p)
		}
	case *Param:
		walkIdent(v, n.Name)
		walkType(v, n.Type)
		walkExpr(v, n.Default)
		walkPersonal(v, n.Personal)
	case *Personal:
		if n.Subject != nil {
			Walk(v, n.Subject)
		}
	case *SortKey:
		Walk(v, n.Path)
	case *ArgList:
		for _, a := range n.Args {
			Walk(v, a)
		}
	case *Arg:
		walkIdent(v, n.Name)
		walkExpr(v, n.Value)

	// Declarations
	case *RoleDecl:
		walkDoc(v, n.Doc)
		walkIdent(v, n.Name)
	case *FeatureDecl:
		walkDoc(v, n.Doc)
		walkIdent(v, n.Name)
		walkExpr(v, n.Date)
	case *TestFeatures:
		for _, x := range n.Names {
			walkIdent(v, x)
		}
	case *EnumDecl:
		walkDoc(v, n.Doc)
		walkIdent(v, n.Name)
		for _, x := range n.Values {
			Walk(v, x)
		}
	case *EnumValue:
		walkDoc(v, n.Doc)
		walkIdent(v, n.Name)
	case *ValueDecl:
		walkDoc(v, n.Doc)
		walkIdent(v, n.Name)
		walkMembers(v, n.Members)
	case *EntityDecl:
		walkDoc(v, n.Doc)
		if n.Owner != nil {
			walkIdent(v, n.Owner)
		}
		walkIdent(v, n.Name)
		walkMembers(v, n.Members)
	case *ProjectionDecl:
		walkDoc(v, n.Doc)
		walkIdent(v, n.Name)
		walkMembers(v, n.Members)
	case *ExternalDecl:
		walkDoc(v, n.Doc)
		walkAccess(v, n.Access)
		walkIdent(v, n.Name)
		walkMembers(v, n.Members)
	case *FakeDecl:
		walkDoc(v, n.Doc)
		walkIdent(v, n.Name)
		walkMembers(v, n.Members)
	case *ACLDecl:
		walkDoc(v, n.Doc)
		walkIdent(v, n.Source)
		walkMembers(v, n.Members)
	case *AppDecl:
		walkDoc(v, n.Doc)
		walkAccess(v, n.Access)
		walkIdent(v, n.Name)
		walkLit(v, n.Title)
		for _, it := range n.Items {
			Walk(v, it)
		}
	case *ScreenDecl:
		walkDoc(v, n.Doc)
		walkIdent(v, n.Name)
		walkParams(v, n.Params)
		walkLit(v, n.Title)
		walkNodes(v, n.Nodes)
	case *DefaultDecl:
		walkDoc(v, n.Doc)
		walkType(v, n.Type)
		walkIdent(v, n.Prim)
		walkArgs(v, n.Args)
	case *PrimitiveDecl:
		walkDoc(v, n.Doc)
		walkIdent(v, n.Name)
		if n.TypeParams != nil {
			Walk(v, n.TypeParams)
		}
		if n.Params != nil {
			Walk(v, n.Params)
		}
		for _, it := range n.Items {
			Walk(v, it)
		}
	case *TextsDecl:
		walkDoc(v, n.Doc)
		walkIdent(v, n.Lang)
		for _, e := range n.Entries {
			Walk(v, e)
		}
	case *MigrationDecl:
		walkDoc(v, n.Doc)
		walkIdent(v, n.Name)
		walkBlock(v, n.Body)
	case *TestDecl:
		walkDoc(v, n.Doc)
		walkExpr(v, n.Name)
		if n.Features != nil {
			Walk(v, n.Features)
		}
		for _, s := range n.Setup {
			Walk(v, s)
		}
		if n.Given != nil {
			Walk(v, n.Given)
		}
		if n.When != nil {
			Walk(v, n.When)
		}
		if n.Then != nil {
			Walk(v, n.Then)
		}
	case *ExtendDecl:
		walkDoc(v, n.Doc)
		walkIdent(v, n.Name)
		walkMembers(v, n.Members)
		for _, in := range n.Ins {
			Walk(v, in)
		}
	case *ExtendIn:
		walkIdent(v, n.Part)
		walkNodes(v, n.Nodes)
	case *ExcludeDecl:
		walkExpr(v, n.Test)
		walkExpr(v, n.Reason)
	case *LetDecl:
		walkDoc(v, n.Doc)
		Walk(v, n.Let)

	// Members
	case *IDDecl:
		walkDoc(v, n.Doc)
		walkType(v, n.Type)
	case *FieldDecl:
		walkDoc(v, n.Doc)
		walkIdent(v, n.Name)
		walkType(v, n.Type)
		walkExpr(v, n.Default)
		walkPersonal(v, n.Personal)
		walkIdent(v, n.Was)
	case *IndexDecl:
		walkDoc(v, n.Doc)
		walkIdent(v, n.Name)
		for _, k := range n.Keys {
			Walk(v, k)
		}
		walkIdent(v, n.Error)
	case *CommandDecl:
		walkDoc(v, n.Doc)
		walkAccess(v, n.Access)
		walkIdent(v, n.Name)
		walkParams(v, n.Params)
		walkBlock(v, n.Body)
	case *EventDecl:
		walkDoc(v, n.Doc)
		walkIdent(v, n.Name)
		walkParams(v, n.Params)
		walkBlock(v, n.Body)
	case *NotificationDecl:
		walkDoc(v, n.Doc)
		walkIdent(v, n.Name)
		walkParams(v, n.Params)
	case *ErrorDecl:
		walkDoc(v, n.Doc)
		walkIdent(v, n.Name)
		walkParams(v, n.Params)
	case *QueryDecl:
		walkDoc(v, n.Doc)
		walkAccess(v, n.Access)
		walkIdent(v, n.Name)
		walkParams(v, n.Params)
		walkType(v, n.Result)
		walkBlock(v, n.Body)
	case *RuleDecl:
		walkDoc(v, n.Doc)
		walkIdent(v, n.Name)
		walkParams(v, n.Params)
		walkType(v, n.Result)
		walkBlock(v, n.Body)
	case *OnDecl:
		walkDoc(v, n.Doc)
		for _, id := range n.Path {
			walkIdent(v, id)
		}
		walkIdent(v, n.Event)
		walkBlock(v, n.Body)
	case *ActorAsDecl:
		walkDoc(v, n.Doc)
		for _, id := range n.From {
			walkIdent(v, id)
		}
		walkIdent(v, n.To)
	case *CheckDecl:
		walkDoc(v, n.Doc)
		walkExpr(v, n.Cond)
	case *MethodDecl:
		walkDoc(v, n.Doc)
		walkIdent(v, n.Name)
		walkParams(v, n.Params)
		walkType(v, n.Result)
		walkBlock(v, n.Body)
	case *RequestDecl:
		walkDoc(v, n.Doc)
		walkIdent(v, n.Name)
		walkParams(v, n.Params)
		walkExpr(v, n.Timeout)
		if n.Body != nil {
			walkBlock(v, n.Body)
		}

	// UI
	case *LanguageItem:
		walkIdent(v, n.Name)
	case *PlatformsItem:
		for _, id := range n.Names {
			walkIdent(v, id)
		}
	case *PrimNode:
		walkIdent(v, n.Name)
		walkArgs(v, n.Args)
		if n.Binding != nil {
			Walk(v, n.Binding)
		}
		walkLit(v, n.Label)
		walkExpr(v, n.Cond)
		walkNodes(v, n.Children)
	case *Binding:
		if n.Target != nil {
			Walk(v, n.Target)
		}
		walkIdent(v, n.Type)
		walkIdent(v, n.Method)
		walkArgs(v, n.Args)
	case *FieldNode:
		walkIdent(v, n.Name)
		walkIdent(v, n.Prim)
		walkArgs(v, n.PrimArgs)
		walkLit(v, n.Label)
		walkExpr(v, n.Cond)
		walkNodes(v, n.Children)
	case *OnNode:
		walkIdent(v, n.Event)
		walkIdent(v, n.Screen)
		walkArgs(v, n.Args)
	case *BadNode:
	case *TypeParamList:
		for _, tp := range n.List {
			Walk(v, tp)
		}
	case *TypeParam:
		walkIdent(v, n.Name)
		walkIdent(v, n.Constraint)
	case *PrimParamList:
		for _, pp := range n.List {
			Walk(v, pp)
		}
	case *PrimParam:
		walkIdent(v, n.Name)
		walkParams(v, n.Query)
		walkType(v, n.Type)
		walkExpr(v, n.Default)
	case *BindsItem:
		walkType(v, n.Type)
	case *PartItem:
		walkIdent(v, n.Name)
		walkType(v, n.Type)
	case *PrimEvent:
		walkIdent(v, n.Name)
		walkParams(v, n.Params)
	case *TextEntry:
		Walk(v, n.Key)
		walkExpr(v, n.Value)
	case *TextKey:
		Walk(v, n.Path)
		if n.Node != nil {
			Walk(v, n.Node)
		}
		walkLit(v, n.N)

	// Statements
	case *Block:
		for _, s := range n.List {
			Walk(v, s)
		}
	case *LetStmt:
		walkIdent(v, n.Name)
		walkType(v, n.Type)
		walkExpr(v, n.Value)
	case *AssignStmt:
		walkExpr(v, n.Target)
		walkExpr(v, n.Value)
	case *IfStmt:
		walkExpr(v, n.Cond)
		walkBlock(v, n.Body)
		if n.Else != nil {
			Walk(v, n.Else)
		}
	case *ForStmt:
		walkIdent(v, n.Var)
		walkExpr(v, n.X)
		walkBlock(v, n.Body)
	case *WhileStmt:
		walkExpr(v, n.Cond)
		walkBlock(v, n.Body)
	case *ReturnStmt:
		walkExpr(v, n.Result)
	case *ExprStmt:
		walkExpr(v, n.X)
		walkExpr(v, n.To)
	case *DeleteStmt:
		walkExpr(v, n.X)
	case *ForgetStmt:
		walkExpr(v, n.X)

	// Expressions
	case *InterpString:
		for _, p := range n.Parts {
			walkExpr(v, p.X)
		}
	case *ParenExpr:
		walkExpr(v, n.X)
	case *UnaryExpr:
		walkExpr(v, n.X)
	case *BinaryExpr:
		walkExpr(v, n.X)
		walkExpr(v, n.Y)
	case *IsNoneExpr:
		walkExpr(v, n.X)
	case *InExpr:
		walkExpr(v, n.X)
		walkExpr(v, n.Y)
	case *SelectorExpr:
		walkExpr(v, n.X)
		walkIdent(v, n.Sel)
	case *CallExpr:
		walkExpr(v, n.Fun)
		walkArgs(v, n.Args)
	case *BracketExpr:
		for _, e := range n.Elems {
			Walk(v, e)
		}
	case *MapLit:
		for _, e := range n.Entries {
			Walk(v, e)
		}
	case *MapEntry:
		walkExpr(v, n.Key)
		walkExpr(v, n.Value)
	case *SearchExpr:
		walkIdent(v, n.Type)
		walkExpr(v, n.Where)
		for _, k := range n.Sort {
			Walk(v, k)
		}
		walkExpr(v, n.Page)
		walkExpr(v, n.Limit)
		walkExpr(v, n.Select)
	case *NewIDExpr:
		if n.Type != nil {
			Walk(v, n.Type)
		}
	case *AllExpr:
		walkIdent(v, n.Type)

	// Types
	case *NamedType:
		for _, e := range n.Path {
			Walk(v, e)
		}
	case *GenericType:
		walkIdent(v, n.Name)
		for _, a := range n.Args {
			Walk(v, a)
		}
	case *OptionalType:
		walkType(v, n.Elem)
	case *UnionType:
		for _, t := range n.Types {
			walkType(v, t)
		}

	// Tests
	case *GivenBlock:
		for _, s := range n.Steps {
			Walk(v, s)
		}
	case *WhenBlock:
		for _, s := range n.Steps {
			Walk(v, s)
		}
	case *ThenBlock:
		for _, it := range n.Items {
			Walk(v, it)
		}
	case *TestCall:
		walkExpr(v, n.Call)
		walkIdent(v, n.As)
	case *TimeOffset:
		walkExpr(v, n.Duration)
	case *TestStmt:
		Walk(v, n.Stmt)
	case *ThenExpr:
		walkExpr(v, n.X)
	case *ThenNotification:
		walkIdent(v, n.Name)
		walkArgs(v, n.Args)
		walkExpr(v, n.To)
	case *ThenError:
		walkIdent(v, n.Name)
		walkArgs(v, n.Args)
	case *ThenRequest:
		walkIdent(v, n.System)
		walkIdent(v, n.Name)
		walkArgs(v, n.Args)
	case *ThenMatches:
		if n.Call != nil {
			Walk(v, n.Call)
		}
		walkArgs(v, n.Fields)

	default:
		panic(fmt.Sprintf("ast.Walk: unknown node %T", n))
	}
}

func walkMembers(v Visitor, list []Member) {
	for _, m := range list {
		Walk(v, m)
	}
}

func walkNodes(v Visitor, list []UINode) {
	for _, n := range list {
		Walk(v, n)
	}
}

// walkLit visits an optional string or number: a nil *BasicLit in an Expr is not nil.
func walkLit(v Visitor, x *BasicLit) {
	if x != nil {
		Walk(v, x)
	}
}
