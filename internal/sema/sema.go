// Package sema collects the symbols of a model and resolves names
// (RFC §28):
//
//  1. global names: declaring types, roles, migrations, tests (E0101);
//  2. the members of each type by namespace (E0102);
//  3. the types of fields, parameters and results, identifiers (E0107–E0111);
//  4. the names of parameters and variables: they do not match fields, do
//     not repeat, do not shadow, do not take built-in names (E0103, E0104, E0108);
//  5. name style (E0106);
//  6. access lists (E06xx).
//
// Names in expressions are resolved together with type checking (stage 6).
package sema

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/constant"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// Info is the result of the analysis: the model symbols and the resolved names.
type Info struct {
	// Decls are the declaring types by name; DeclList is in declaration order.
	Decls    map[string]*types.Decl
	DeclList []*types.Decl
	short    declIndex // DeclList by short name (index.go)
	// Roles are the project roles by name: roles are shared by all bounded contexts.
	Roles map[string]*ast.RoleDecl
	// RoleNames are the role names.
	RoleNames map[*ast.RoleDecl]string
	// Features are the feature flags by full name ("Ordering.tips"; "tips"
	// in a flat module); FeatureNames are the full names of the declarations.
	Features     map[string]*ast.FeatureDecl
	FeatureNames map[*ast.FeatureDecl]string
	// TestFeatures are the flags a test turns on with a feature line: full names.
	TestFeatures map[*ast.TestDecl][]string
	// PerfMethods are the methods of stats.of(Type.method) in then of
	// performance tests: full names.
	PerfMethods map[*ast.CallExpr]string
	// On are the ACL reactions to events of the source bounded context (on
	// Ordering.Order.placed): the entity and the event.
	On map[*ast.OnDecl]*OnEvent
	// ACLs are the anti-corruption layers in declaration order.
	ACLs []*ACL
	// TypeExprs are the resolved type expressions.
	TypeExprs map[ast.TypeExpr]types.Type
	// Types are the types of expressions in bodies.
	Types map[ast.Expr]types.Type
	// Uses are what names in expressions refer to.
	Uses map[*ast.Ident]*Object
	// Calls are the resolved calls.
	Calls map[*ast.CallExpr]*Call
	// UI is the UI: apps, screens, primitives, texts (RFC §22).
	UI *UI
	// TestAs is on whose behalf a call in a test is made.
	TestAs map[*ast.TestCall]Access
	// Then is what then checks refer to: an error, a notification, an external system request.
	Then map[ast.ThenItem]*types.Method
	// Access holds the resolved access list elements.
	Access map[*ast.Ident]Access
	// Owner is the declaration a member belongs to.
	Owner map[ast.Member]*types.Decl
	// Defaults are the default values: fields of entities, projections and
	// value objects (explicit or the type's value), parameters with "= value".
	Defaults map[*types.Var]constant.Value
	// TypeRefs are the declaring types of searches [Type] and all Type.
	TypeRefs map[ast.Expr]*types.Decl

	scope *checker // name lookup by namespace (Resolve, Namespace)
	// Impls are request implementations in fakes: the declared system request.
	Impls map[*ast.RequestDecl]*types.Method
}

// OnEvent is the event of an ACL reaction: the source entity and its event.
type OnEvent struct {
	Model *types.Decl
	Event *types.Method
}

// AccessKind is what an access list element turned out to be.
type AccessKind uint8

const (
	AccessSelf AccessKind = iota
	AccessField
	AccessActor
	AccessRole
)

// Access is a resolved access list element.
type Access struct {
	Kind  AccessKind
	Field *types.Var    // AccessField
	Actor *types.Decl   // AccessActor
	Role  *ast.RoleDecl // AccessRole
}

type checker struct {
	prog     *module.Program
	info     *Info
	bag      diag.Bag
	files    []*source.File
	orphans  map[ast.Member]*types.Method // repeated methods that did not go into the type
	std      map[ast.Node]bool            // declarations of the standard UI catalog
	stdPrims []*types.Decl                // catalog primitives: they are not in Decls
	fixtures map[string]*Object           // shared test lets
	// Default values of value objects: the computation state and the value.
	defaults      map[*types.Decl]int
	defaultValues map[*types.Decl]constant.Struct
	evaluating    map[*types.Var]bool // fields whose value is being computed now
	// Namespaces (scope.go): files with bounded contexts, bounded contexts,
	// ACLs and the places E1407 was already reported for.
	ctxFiles []ctxFile
	contexts map[string]bool
	aclDecls []*ast.ACLDecl
	reported map[source.Pos]bool
	// Declaration indexes (index.go).
	methods methodIndex
	aggs    aggIndex
}

// Check analyzes an effective model.
func Check(p *module.Program) (*Info, []*diag.Diagnostic) {
	c := &checker{info: &Info{
		Decls:        map[string]*types.Decl{},
		Roles:        map[string]*ast.RoleDecl{},
		RoleNames:    map[*ast.RoleDecl]string{},
		Features:     map[string]*ast.FeatureDecl{},
		FeatureNames: map[*ast.FeatureDecl]string{},
		TestFeatures: map[*ast.TestDecl][]string{},
		On:           map[*ast.OnDecl]*OnEvent{},
		PerfMethods:  map[*ast.CallExpr]string{},
		TypeExprs:    map[ast.TypeExpr]types.Type{},
		Types:        map[ast.Expr]types.Type{},
		Uses:         map[*ast.Ident]*Object{},
		Calls:        map[*ast.CallExpr]*Call{},
		TestAs:       map[*ast.TestCall]Access{},
		Then:         map[ast.ThenItem]*types.Method{},
		Access:       map[*ast.Ident]Access{},
		Owner:        map[ast.Member]*types.Decl{},
		Defaults:     map[*types.Var]constant.Value{},
		Impls:        map[*ast.RequestDecl]*types.Method{},
		TypeRefs:     map[ast.Expr]*types.Decl{},
	}}
	c.prog = p
	c.orphans = map[ast.Member]*types.Method{}
	c.std = map[ast.Node]bool{}
	for _, d := range p.Std {
		c.std[d] = true
	}
	for _, f := range p.Files {
		c.files = append(c.files, f.Src)
	}
	c.initScope()
	c.info.scope = c
	c.collectStd(p.Std)
	c.collectGlobals(p.Decls)
	for _, d := range c.info.DeclList {
		c.resolveID(d)
	}
	for _, d := range c.info.DeclList {
		c.collectMembers(d)
	}
	c.checkInnerIDs()
	c.checkLocals(p.Decls)
	for _, d := range c.info.DeclList {
		c.resolveAccess(d)
	}
	c.checkACLs()
	c.systemAnswers()
	c.checkBodies(p.Decls)
	c.checkUI()
	c.checkFeatureTests()
	list := c.bag.List()
	diag.Sort(list)
	return c.info, list
}

func rangeOf(n ast.Node) source.Range { return source.Range{Start: n.Pos(), End: n.End()} }

var (
	noteDeclared = "declared here"
	noteStd      = "declared in the standard UI catalog"
	textUpper    = "an uppercase"
	textLower    = "a lowercase"
)

// collectGlobals collects declaring types and roles; it checks that the
// names of migrations and tests do not repeat.
func (c *checker) collectGlobals(decls []ast.Decl) {
	first := map[string]*ast.Ident{} // global name → first declaration
	var inner []*types.Decl          // inner entities of aggregates
	migrations := map[string]*ast.Ident{}
	tests := map[string]*ast.BasicLit{}
	for _, d := range decls {
		switch d := d.(type) {
		case *ast.MigrationDecl:
			if prev := migrations[d.Name.Name]; prev != nil {
				c.bag.Add(diag.E0101, rangeOf(d.Name), d.Name.Name, "migration names must be unique").
					Note(rangeOf(prev), noteDeclared)
				continue
			}
			migrations[d.Name.Name] = d.Name
			continue
		case *ast.TestDecl:
			if prev := tests[d.Name.Value]; prev != nil {
				c.bag.Add(diag.E0101, rangeOf(d.Name), d.Name.Value, "test names must be unique").
					Note(rangeOf(prev), noteDeclared)
				continue
			}
			tests[d.Name.Value] = d.Name
			continue
		}
		if f, ok := d.(*ast.FeatureDecl); ok {
			c.collectFeature(f)
			continue
		}
		if r, ok := d.(*ast.RoleDecl); ok {
			c.lowerName(r.Name, kindRole)
			full := r.Name.Name
			if prev, dup := c.info.Roles[full]; dup {
				c.bag.Add(diag.E0101, rangeOf(r.Name), r.Name.Name, "roles of a project must be unique").
					Note(rangeOf(prev.Name), noteDeclared)
				continue
			}
			c.info.Roles[full] = r
			c.info.RoleNames[r] = full
			continue
		}
		kind, name, ok := declKind(d)
		if !ok {
			continue
		}
		c.upperName(name, kindDecl[kind])
		ctx := c.ctxOf(d)
		full := qualify(ctx, name.Name)
		if e, ok := d.(*ast.EntityDecl); ok && e.Owner != nil {
			full = qualify(ctx, e.Owner.Name+"."+name.Name)
		} else if ok && !e.IsAggregate && !e.IsActor {
			c.topEntity(e)
		}
		if prev := first[full]; prev != nil {
			c.bag.Add(diag.E0101, rangeOf(name), name.Name,
				"names of entities, projections, values, enums, external systems, apps, screens and primitives must be unique").
				Note(rangeOf(prev), noteDeclared)
			continue
		}
		if kind.IsUI() {
			if std := c.stdPrim(name.Name); std != nil {
				c.bag.Add(diag.E0101, rangeOf(name), name.Name,
					"names of the standard UI catalog cannot be redeclared").
					Note(rangeOf(std.Node.(*ast.PrimitiveDecl).Name), noteStd)
				continue
			}
		}
		first[full] = name
		td := &types.Decl{Kind: kind, Name: full, Short: name.Name, Context: ctx, Node: d}
		c.info.Decls[full] = td
		c.info.DeclList = append(c.info.DeclList, td)
		if e, ok := d.(*ast.EntityDecl); ok && e.Owner != nil {
			inner = append(inner, td)
		}
	}
	c.linkInner(inner)
	c.checkNamespaces()
}

// collectStd collects the primitives of the standard catalog. They are not
// in the model's global namespace: a model type may be named like a catalog
// primitive (value Row), since a type and a primitive are written in
// different places; a screen, an app and an own primitive may not.
func (c *checker) collectStd(decls []ast.Decl) {
	for _, d := range decls {
		if n, ok := d.(*ast.PrimitiveDecl); ok {
			c.stdPrims = append(c.stdPrims, &types.Decl{Kind: types.PrimitiveDecl, Name: n.Name.Name, Node: n})
		}
	}
}

func (c *checker) stdPrim(name string) *types.Decl {
	for _, d := range c.stdPrims {
		if d.Name == name {
			return d
		}
	}
	return nil
}

// declKind returns the kind and the name of a declaring type.
func declKind(d ast.Decl) (types.DeclKind, *ast.Ident, bool) {
	switch d := d.(type) {
	case *ast.EntityDecl:
		if d.IsActor {
			return types.ActorDecl, d.Name, true
		}
		return types.EntityDecl, d.Name, true
	case *ast.ProjectionDecl:
		return types.ProjectionDecl, d.Name, true
	case *ast.ValueDecl:
		return types.ValueDecl, d.Name, true
	case *ast.EnumDecl:
		return types.EnumDecl, d.Name, true
	case *ast.ExternalDecl:
		return types.ExternalDecl, d.Name, true
	case *ast.AppDecl:
		return types.AppDecl, d.Name, true
	case *ast.ScreenDecl:
		return types.ScreenDecl, d.Name, true
	case *ast.PrimitiveDecl:
		return types.PrimitiveDecl, d.Name, true
	}
	return 0, nil, false
}

// Kind names for name style messages.
var (
	kindDecl = map[types.DeclKind]string{
		types.EntityDecl:     "Entity name",
		types.ActorDecl:      "Actor name",
		types.ProjectionDecl: "Projection name",
		types.ValueDecl:      "Value type name",
		types.EnumDecl:       "Enum name",
		types.ExternalDecl:   "External system name",
		types.AppDecl:        "App name",
		types.ScreenDecl:     "Screen name",
		types.PrimitiveDecl:  "Primitive name",
	}
	kindRole      = "Role name"
	kindFeature   = "Flag name"
	kindField     = "Field name"
	kindParam     = "Parameter name"
	kindVar       = "Variable name"
	kindIndex     = "Index name"
	kindEvent     = "Event name"
	kindNotif     = "Notification name"
	kindError     = "Error name"
	kindRule      = "Rule name"
	kindMethod    = "Method name"
	kindRequest   = "External request name"
	kindEnumValue = "Enum value"
	kindTypeVar   = "Type variable"
	kindPart      = "Part name"
)

func (c *checker) upperName(id *ast.Ident, kind string) {
	if id.Name != "" && !types.IsUpper(id.Name) {
		c.bag.Add(diag.E0106, rangeOf(id), kind, id.Name, textUpper)
	}
}

func (c *checker) lowerName(id *ast.Ident, kind string) {
	if id.Name != "" && types.IsUpper(id.Name) {
		c.bag.Add(diag.E0106, rangeOf(id), kind, id.Name, textLower)
	}
}
