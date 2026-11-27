package ast

import "github.com/tycheg/reasonr-compiler/internal/source"

// RoleDecl — role admin.
type RoleDecl struct {
	Doc  *CommentGroup
	Role source.Pos
	Name *Ident
}

// FeatureDecl is a system feature flag: feature tips "2026-11-07" (the date it was introduced).
type FeatureDecl struct {
	Doc     *CommentGroup
	Feature source.Pos
	Name    *Ident
	Date    *BasicLit // a YYYY-MM-DD string
}

// EnumDecl is an enum.
type EnumDecl struct {
	Doc    *CommentGroup
	Enum   source.Pos
	Name   *Ident
	Values []*EnumValue
	EndPos source.Pos
}

// EnumValue is an enum value.
type EnumValue struct {
	Doc  *CommentGroup
	Name *Ident
	Was  *Ident // the old name for clients of earlier API versions (RFC §31.3.1); nil when none
}

func (v *EnumValue) Pos() source.Pos { return v.Name.Pos() }
func (v *EnumValue) End() source.Pos {
	if v.Was != nil {
		return v.Was.End()
	}
	return v.Name.End()
}

// ValueDecl is a value object: fields, check invariants and methods.
type ValueDecl struct {
	Doc     *CommentGroup
	Value   source.Pos
	Name    *Ident
	Members []Member
	EndPos  source.Pos
}

// EntityDecl is an aggregate (aggregate), an actor (actor) or an inner
// entity of an aggregate (entity Order.Stop, docs/plans/0011, §3.5).
type EntityDecl struct {
	Doc         *CommentGroup
	Keyword     source.Pos // aggregate, actor or entity
	IsActor     bool
	IsAggregate bool
	Owner       *Ident // the aggregate of an inner entity: Order in entity Order.Stop; nil when none
	Name        *Ident
	Members     []Member
	EndPos      source.Pos
}

// Inner reports that this is an inner entity of an aggregate.
func (d *EntityDecl) Inner() bool { return d.Owner != nil }

// ProjectionDecl is a projection.
type ProjectionDecl struct {
	Doc        *CommentGroup
	Projection source.Pos
	Name       *Ident
	Members    []Member
	EndPos     source.Pos
}

// ExternalDecl is an external system with requests: external[(role)] Name.
type ExternalDecl struct {
	Doc      *CommentGroup
	External source.Pos
	Access   *AccessList // the role on whose behalf the system answers, or nil
	Name     *Ident
	Members  []Member // *RequestDecl or *BadMember
	EndPos   source.Pos
}

// FakeDecl is a fake implementation of an external system for tests
// (*.fake.rr files): fields, events, commands and request implementations.
type FakeDecl struct {
	Doc     *CommentGroup
	Fake    source.Pos
	Name    *Ident
	Members []Member // *FieldDecl, *EventDecl, *CommandDecl, *RequestDecl with a body, or *BadMember
	EndPos  source.Pos
}

// ACLDecl is the anti-corruption layer of a bounded context (*.acl.rr
// files): it translates the model of the source bounded context into the
// model of its own one (docs/plans/0011-contexts-acl-aggregates.md, §3.1).
type ACLDecl struct {
	Doc     *CommentGroup
	ACL     source.Pos
	Source  *Ident   // the source bounded context
	Members []Member // *OnDecl, *ActorAsDecl or *BadMember
	EndPos  source.Pos
}

// MigrationDecl is a manual migration.
type MigrationDecl struct {
	Doc       *CommentGroup
	Migration source.Pos
	Name      *Ident
	Body      *Block
}

// TestDecl is an acceptance test.
type TestDecl struct {
	Doc      *CommentGroup
	Test     source.Pos
	Name     *BasicLit     // a string
	Features *TestFeatures // the feature line at the start of the test, or nil
	Setup    []Stmt        // let before the sections
	Given    *GivenBlock
	When     *WhenBlock
	Then     *ThenBlock
	EndPos   source.Pos
}

// TestFeatures is feature tips, Auth.newLogin: flags turned on for the whole
// system in a test. A flag of another system is one dotted name.
type TestFeatures struct {
	Feature source.Pos
	Names   []*Ident
}

func (f *TestFeatures) Pos() source.Pos { return f.Feature }
func (f *TestFeatures) End() source.Pos {
	if n := len(f.Names); n > 0 {
		return f.Names[n-1].End()
	}
	return f.Feature + source.Pos(len("feature"))
}

// ExtendDecl is an extension: extend Order or extend screen MyOrder.
type ExtendDecl struct {
	Doc       *CommentGroup
	Extend    source.Pos
	ScreenPos source.Pos // position of screen if a screen is extended
	Name      *Ident
	Members   []Member    // for a type
	Ins       []*ExtendIn // for a screen
	EndPos    source.Pos
}

// IsScreen reports whether a screen is extended.
func (d *ExtendDecl) IsScreen() bool { return d.ScreenPos.IsValid() }

// ExtendIn holds the lines a screen extension adds to a part: in Actions.
type ExtendIn struct {
	In     source.Pos
	Part   *Ident
	Nodes  []UINode
	EndPos source.Pos
}

func (i *ExtendIn) Pos() source.Pos { return i.In }
func (i *ExtendIn) End() source.Pos { return i.EndPos }

// ExcludeDecl — exclude test "…" because "…".
type ExcludeDecl struct {
	Exclude source.Pos
	TestPos source.Pos
	Test    *BasicLit
	Because source.Pos
	Reason  *BasicLit
}

// LetDecl is a top-level let (shared test fixtures).
type LetDecl struct {
	Doc *CommentGroup
	Let *LetStmt
}

// BadDecl is a declaration that could not be parsed.
type BadDecl struct {
	From, To source.Pos
}

func (d *RoleDecl) Pos() source.Pos       { return d.Role }
func (d *FeatureDecl) Pos() source.Pos    { return d.Feature }
func (d *EnumDecl) Pos() source.Pos       { return d.Enum }
func (d *ValueDecl) Pos() source.Pos      { return d.Value }
func (d *EntityDecl) Pos() source.Pos     { return d.Keyword }
func (d *ProjectionDecl) Pos() source.Pos { return d.Projection }
func (d *ExternalDecl) Pos() source.Pos   { return d.External }
func (d *FakeDecl) Pos() source.Pos       { return d.Fake }
func (d *ACLDecl) Pos() source.Pos        { return d.ACL }
func (d *MigrationDecl) Pos() source.Pos  { return d.Migration }
func (d *TestDecl) Pos() source.Pos       { return d.Test }
func (d *ExtendDecl) Pos() source.Pos     { return d.Extend }
func (d *ExcludeDecl) Pos() source.Pos    { return d.Exclude }
func (d *LetDecl) Pos() source.Pos        { return d.Let.Pos() }
func (d *BadDecl) Pos() source.Pos        { return d.From }

func (d *RoleDecl) End() source.Pos       { return d.Name.End() }
func (d *FeatureDecl) End() source.Pos    { return d.Date.End() }
func (d *EnumDecl) End() source.Pos       { return d.EndPos }
func (d *ValueDecl) End() source.Pos      { return d.EndPos }
func (d *EntityDecl) End() source.Pos     { return d.EndPos }
func (d *ProjectionDecl) End() source.Pos { return d.EndPos }
func (d *ExternalDecl) End() source.Pos   { return d.EndPos }
func (d *FakeDecl) End() source.Pos       { return d.EndPos }
func (d *ACLDecl) End() source.Pos        { return d.EndPos }
func (d *MigrationDecl) End() source.Pos  { return d.Body.End() }
func (d *TestDecl) End() source.Pos       { return d.EndPos }
func (d *ExtendDecl) End() source.Pos     { return d.EndPos }
func (d *ExcludeDecl) End() source.Pos    { return d.Reason.End() }
func (d *LetDecl) End() source.Pos        { return d.Let.End() }
func (d *BadDecl) End() source.Pos        { return d.To }

func (*RoleDecl) declNode()       {}
func (*FeatureDecl) declNode()    {}
func (*EnumDecl) declNode()       {}
func (*ValueDecl) declNode()      {}
func (*EntityDecl) declNode()     {}
func (*ProjectionDecl) declNode() {}
func (*ExternalDecl) declNode()   {}
func (*FakeDecl) declNode()       {}
func (*ACLDecl) declNode()        {}
func (*MigrationDecl) declNode()  {}
func (*TestDecl) declNode()       {}
func (*ExtendDecl) declNode()     {}
func (*ExcludeDecl) declNode()    {}
func (*LetDecl) declNode()        {}
func (*BadDecl) declNode()        {}
