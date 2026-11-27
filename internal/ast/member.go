package ast

import "github.com/tycheg/reasonr-compiler/internal/source"

// IDDecl is id uuid | sequence | string | Composite | Order.Id.
type IDDecl struct {
	Doc  *CommentGroup
	ID   source.Pos
	Type TypeExpr
}

// FieldDecl is a field: name Type [= value] [personal[(subject)]] [was oldName].
type FieldDecl struct {
	Doc      *CommentGroup
	Name     *Ident
	Type     TypeExpr
	Default  Expr      // or nil
	Personal *Personal // or nil
	WasPos   source.Pos
	Was      *Ident // or nil
}

// IndexDecl is index name(keys) [unique error].
type IndexDecl struct {
	Doc       *CommentGroup
	Index     source.Pos
	Name      *Ident
	Lparen    source.Pos
	Keys      []*SortKey
	Rparen    source.Pos
	UniquePos source.Pos
	Error     *Ident // the error on a uniqueness violation, or nil
}

// IsUnique reports whether the index is unique.
func (d *IndexDecl) IsUnique() bool { return d.UniquePos.IsValid() }

// CommandDecl is a command.
type CommandDecl struct {
	Doc     *CommentGroup
	Command source.Pos
	Access  *AccessList // or nil
	// Create is the position of the word create: a creation command whose
	// identifier the engine issues (docs/plans/0011, §3.8); NoPos for a
	// regular command.
	Create source.Pos
	Name   *Ident
	// Was is the old name of a public command for clients of earlier API
	// versions (… was Cancel, RFC §31.3.1); nil when none.
	Was    *Ident
	Params *ParamList
	Body   *Block
}

// EventDecl is an event; the body is optional.
type EventDecl struct {
	Doc    *CommentGroup
	Event  source.Pos
	Name   *Ident
	Params *ParamList
	Body   *Block // or nil
}

// NotificationDecl is a notification.
type NotificationDecl struct {
	Doc          *CommentGroup
	Notification source.Pos
	Name         *Ident
	Params       *ParamList
}

// ErrorDecl is an error; the parameters are optional.
type ErrorDecl struct {
	Doc    *CommentGroup
	Error  source.Pos
	Name   *Ident
	Params *ParamList // or nil
}

// QueryDecl is a query.
type QueryDecl struct {
	Doc    *CommentGroup
	Query  source.Pos
	Access *AccessList // or nil
	Name   *Ident
	Params *ParamList
	Result TypeExpr
	Was    *Ident // the old name for clients of earlier API versions; nil when none
	Body   *Block
}

// RuleDecl is a rule (an extension point).
type RuleDecl struct {
	Doc    *CommentGroup
	Rule   source.Pos
	Name   *Ident
	Params *ParamList
	Result TypeExpr
	Body   *Block
}

// OnDecl is a reaction to an event: on placed (extensions) or
// on Ordering.Order.placed (an event of an entity model in another bounded context).
type OnDecl struct {
	Doc   *CommentGroup
	On    source.Pos
	Path  []*Ident // bounded context (and system) and entity before the event; empty for an own event
	Event *Ident
	Body  *Block
}

// ActorAsDecl is an ACL member: actor Auth.User as Customer — a calling
// actor of the source acts as an actor of its own bounded context with the
// same identifier.
type ActorAsDecl struct {
	Doc   *CommentGroup
	Actor source.Pos
	From  []*Ident // the source actor: Auth.User
	To    *Ident   // the actor of its own bounded context
}

// CheckDecl is a value object invariant: check expression.
type CheckDecl struct {
	Doc   *CommentGroup
	Check source.Pos
	Cond  Expr
}

// MethodDecl is a value object method: name(parameters) Type.
type MethodDecl struct {
	Doc    *CommentGroup
	Name   *Ident
	Params *ParamList
	Result TypeExpr
	Body   *Block
}

// RequestDecl is a request to an external system: request name(parameters).
// In a fake it is a request implementation with a body. Timeout is the old
// form "timeout 30s" (error E1203).
type RequestDecl struct {
	Doc        *CommentGroup
	Request    source.Pos
	Name       *Ident
	Params     *ParamList
	TimeoutPos source.Pos
	Timeout    Expr   // or nil
	Body       *Block // implementation in a fake, or nil
}

// BadMember is a member that could not be parsed.
type BadMember struct {
	From, To source.Pos
}

func (m *IDDecl) Pos() source.Pos           { return m.ID }
func (m *FieldDecl) Pos() source.Pos        { return m.Name.Pos() }
func (m *IndexDecl) Pos() source.Pos        { return m.Index }
func (m *CommandDecl) Pos() source.Pos      { return m.Command }
func (m *EventDecl) Pos() source.Pos        { return m.Event }
func (m *NotificationDecl) Pos() source.Pos { return m.Notification }
func (m *ErrorDecl) Pos() source.Pos        { return m.Error }
func (m *QueryDecl) Pos() source.Pos        { return m.Query }
func (m *RuleDecl) Pos() source.Pos         { return m.Rule }
func (m *OnDecl) Pos() source.Pos           { return m.On }
func (m *CheckDecl) Pos() source.Pos        { return m.Check }
func (m *ActorAsDecl) Pos() source.Pos      { return m.Actor }
func (m *MethodDecl) Pos() source.Pos       { return m.Name.Pos() }
func (m *RequestDecl) Pos() source.Pos      { return m.Request }
func (m *BadMember) Pos() source.Pos        { return m.From }

func (m *IDDecl) End() source.Pos { return m.Type.End() }
func (m *FieldDecl) End() source.Pos {
	switch {
	case m.Was != nil:
		return m.Was.End()
	case m.Personal != nil:
		return m.Personal.End()
	case m.Default != nil:
		return m.Default.End()
	}
	return m.Type.End()
}
func (m *IndexDecl) End() source.Pos {
	if m.Error != nil {
		return m.Error.End()
	}
	return m.Rparen + 1
}
func (m *CommandDecl) End() source.Pos { return m.Body.End() }
func (m *EventDecl) End() source.Pos {
	if m.Body != nil {
		return m.Body.End()
	}
	return m.Params.End()
}
func (m *NotificationDecl) End() source.Pos { return m.Params.End() }
func (m *ErrorDecl) End() source.Pos {
	if m.Params != nil {
		return m.Params.End()
	}
	return m.Name.End()
}
func (m *QueryDecl) End() source.Pos   { return m.Body.End() }
func (m *RuleDecl) End() source.Pos    { return m.Body.End() }
func (m *OnDecl) End() source.Pos      { return m.Body.End() }
func (m *CheckDecl) End() source.Pos   { return m.Cond.End() }
func (m *ActorAsDecl) End() source.Pos { return m.To.End() }
func (m *MethodDecl) End() source.Pos  { return m.Body.End() }
func (m *RequestDecl) End() source.Pos {
	if m.Body != nil {
		return m.Body.End()
	}
	if m.Timeout != nil {
		return m.Timeout.End()
	}
	return m.Params.End()
}
func (m *BadMember) End() source.Pos { return m.To }

func (*IDDecl) memberNode()           {}
func (*FieldDecl) memberNode()        {}
func (*IndexDecl) memberNode()        {}
func (*CommandDecl) memberNode()      {}
func (*EventDecl) memberNode()        {}
func (*NotificationDecl) memberNode() {}
func (*ErrorDecl) memberNode()        {}
func (*QueryDecl) memberNode()        {}
func (*RuleDecl) memberNode()         {}
func (*OnDecl) memberNode()           {}
func (*CheckDecl) memberNode()        {}
func (*ActorAsDecl) memberNode()      {}
func (*MethodDecl) memberNode()       {}
func (*RequestDecl) memberNode()      {}
func (*BadMember) memberNode()        {}
