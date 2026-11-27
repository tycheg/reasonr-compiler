package module

import (
	"fmt"
	"github.com/tycheg/reasonr-compiler/internal/apicontract"
	"sort"
	"sync"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

// Program is an effective model: the base one or the base with an
// extension. Base trees do not change: extended entities and screens are
// copies with added members and lines, so one loaded module can be built
// with different extensions.
type Program struct {
	Extension string  // "" is the base model
	Files     []*File // base files, then extension files
	// Decls are the declarations of the effective model in file order.
	// extend and exclude are not here: they are already applied; excluded
	// tests are absent.
	Decls []ast.Decl
	// Excluded lists the exclusions of base tests that found their test.
	Excluded []*ast.ExcludeDecl
	// Production: the production program, without tests and fakes.
	Production bool
	// Std holds the declarations of the standard UI catalog: primitives and
	// default (RFC §22.8). They are not in Decls.
	Std []ast.Decl

	origin map[ast.Node]bool
	fakes  map[ast.Node]bool
	parts  map[ast.Decl][]ast.Decl // a partial aggregate → its declarations by file
	mod    *Module

	byPos   []*File // Files by start in the FileSet (FileAt)
	byPosMu sync.Mutex
}

// FileAt returns the program file that holds position pos, or nil: a binary
// search (docs/plans/0015).
func (p *Program) FileAt(pos source.Pos) *File {
	if !pos.IsValid() {
		return nil
	}
	p.byPosMu.Lock()
	if len(p.byPos) != len(p.Files) {
		p.byPos = append(p.byPos[:0], p.Files...)
		sort.Slice(p.byPos, func(i, j int) bool { return p.byPos[i].Src.Base() < p.byPos[j].Src.Base() })
	}
	files := p.byPos
	p.byPosMu.Unlock()
	i := sort.Search(len(files), func(i int) bool { return files[i].Src.Base() > int(pos) }) - 1
	if i >= 0 && files[i].Src.Contains(pos) {
		return files[i]
	}
	return nil
}

// API returns the module's external API contract, or nil.
func (p *Program) API() *apicontract.Source {
	if p.mod == nil {
		return nil
	}
	return p.mod.API
}

// Parts returns the declarations a partial aggregate d is built from
// (aggregate Order in several files of a bounded context, docs/plans/0011,
// §4), or d itself.
func (p *Program) Parts(d ast.Decl) []ast.Decl {
	if ps := p.parts[d]; ps != nil {
		return ps
	}
	return []ast.Decl{d}
}

// mergeParts assembles partial aggregates: aggregate (or actor)
// declarations with one name in one bounded context are one aggregate, with
// members in file order. Base trees do not change: the aggregate is a copy
// of the first declaration.
func (p *Program) mergeParts() {
	first := map[string]int{}
	var out []ast.Decl
	for _, d := range p.Decls {
		e, ok := d.(*ast.EntityDecl)
		if !ok || e.Owner != nil || !e.IsAggregate && !e.IsActor || e.Name == nil {
			out = append(out, d)
			continue
		}
		key := p.key(d, e.Name.Name)
		i, seen := first[key]
		if !seen {
			first[key] = len(out)
			out = append(out, d)
			continue
		}
		prev := out[i].(*ast.EntityDecl)
		if prev.IsActor != e.IsActor {
			out = append(out, d) // aggregate and actor with one name are a repeat (E0101)
			continue
		}
		if p.parts == nil {
			p.parts = map[ast.Decl][]ast.Decl{}
		}
		parts := p.parts[prev]
		if parts == nil {
			parts = []ast.Decl{prev}
		}
		merged := *prev
		merged.Members = append(append([]ast.Member(nil), prev.Members...), e.Members...)
		delete(p.parts, prev)
		p.parts[&merged] = append(parts, e)
		out[i] = &merged
	}
	p.Decls = out
}

// Generate parses text built by the compiler as a module file
// (Module.Generate); nil means the program was built without a module.
func (p *Program) Generate(name string, src []byte) (*File, []*diag.Diagnostic) {
	if p.mod == nil || p.mod.fset == nil {
		return nil, nil
	}
	return p.mod.Generate(name, src)
}

// Context returns the bounded context of the file that holds a node:
// "Ordering"; "" for a flat module, apps, primitives and generated files.
func (p *Program) Context(n ast.Node) string {
	if f := p.FileAt(n.Pos()); f != nil {
		return f.Context
	}
	return ""
}

// key is the name of a declaration in its bounded context: same-named
// declarations of different contexts (models of one entity) are different.
func (p *Program) key(d ast.Node, name string) string { return p.Context(d) + "\x00" + name }

// FromExtension reports whether a declaration or member came from an extension.
func (p *Program) FromExtension(n ast.Node) bool { return p.origin[n] }

// FromFake reports whether an external system member came from its fake.
func (p *Program) FromFake(n ast.Node) bool { return p.fakes[n] }

// Program builds the test program with extension name: the model, tests and
// external system fakes (RFC §18); name == "" is the base model.
func (m *Module) Program(name string) (*Program, []*diag.Diagnostic, error) {
	return m.program(name, false)
}

// Production builds the production program with extension name: the model
// without tests and fakes, which the engine runs outside test mode.
func (m *Module) Production(name string) (*Program, []*diag.Diagnostic, error) {
	return m.program(name, true)
}

func (m *Module) program(name string, production bool) (*Program, []*diag.Diagnostic, error) {
	p := &Program{Extension: name, Production: production, origin: map[ast.Node]bool{}, fakes: map[ast.Node]bool{}, mod: m}
	var ext *Extension
	if name != "" {
		if ext = m.Extension(name); ext == nil {
			return nil, nil, fmt.Errorf("module %s has no extension %q (directory %s/%s)", m.Dir, name, ExtensionsDir, name)
		}
	}
	if m.Std != nil {
		p.Std = m.Std.AST.Decls
	}
	var fakes []*ast.FakeDecl
	for _, f := range m.Base {
		if production && (f.Test || f.Fake) {
			continue
		}
		p.Files = append(p.Files, f)
		for _, d := range f.AST.Decls {
			switch d := d.(type) {
			case *ast.ExtendDecl, *ast.ExcludeDecl:
				// Error E1009 was reported on load.
			case *ast.FakeDecl:
				if f.Fake {
					fakes = append(fakes, d)
				}
			default:
				if !f.Fake {
					p.Decls = append(p.Decls, d)
				}
			}
		}
	}
	var bag diag.Bag
	p.mergeFakes(fakes)
	p.mergeParts()
	if ext == nil {
		return p, nil, nil
	}
	mg := newMerger(p)
	for _, f := range ext.Files {
		if production && f.Test {
			continue
		}
		p.Files = append(p.Files, f)
		for _, d := range f.AST.Decls {
			if _, fake := d.(*ast.FakeDecl); !fake {
				mg.decl(d)
			}
		}
	}
	mg.finish()
	bag.Append(mg.bag.List()...)
	return p, bag.List(), nil
}

// mergeFakes adds the members of fakes to copies of their external systems.
// Fakes without a system with a role and repeated ones (errors on load,
// checkFakeSystems) are skipped.
func (p *Program) mergeFakes(fakes []*ast.FakeDecl) {
	index := map[string]int{}
	for i, d := range p.Decls {
		if e, ok := d.(*ast.ExternalDecl); ok {
			if _, dup := index[p.key(e, e.Name.Name)]; !dup {
				index[p.key(e, e.Name.Name)] = i
			}
		}
	}
	seen := map[string]bool{}
	for _, f := range fakes {
		name := p.key(f, f.Name.Name)
		i, ok := index[name]
		if !ok || p.Decls[i].(*ast.ExternalDecl).Access == nil || seen[name] {
			continue
		}
		seen[name] = true
		base := p.Decls[i].(*ast.ExternalDecl)
		cp := *base
		cp.Members = append(append([]ast.Member(nil), base.Members...), f.Members...)
		p.Decls[i] = &cp
		for _, m := range f.Members {
			p.fakes[m] = true
		}
	}
}

// merger applies an extension to the base model.
type merger struct {
	p   *Program
	bag diag.Bag

	byName map[string]int // base declaration name → index in p.Decls
	tests  map[string]int // base test name → index in p.Decls
	// Extended declarations: a copy and the base members by name.
	types   map[string]*extType
	screens map[string]*ast.ScreenDecl
	// Excluded tests.
	excluded map[int]bool
}

type extType struct {
	decl    ast.Decl // a copy of *ast.EntityDecl
	name    *ast.Ident
	base    map[string]ast.Member // members of the base declaration by name
	members *[]ast.Member         // members of the copy
	ons     []*ast.OnDecl         // reactions from the extension
}

func newMerger(p *Program) *merger {
	mg := &merger{
		p:        p,
		byName:   map[string]int{},
		tests:    map[string]int{},
		types:    map[string]*extType{},
		screens:  map[string]*ast.ScreenDecl{},
		excluded: map[int]bool{},
	}
	for i, d := range p.Decls {
		if t, ok := d.(*ast.TestDecl); ok {
			if _, dup := mg.tests[t.Name.Value]; !dup {
				mg.tests[t.Name.Value] = i
			}
			continue
		}
		if name := declName(d); name != nil {
			if _, dup := mg.byName[p.key(d, name.Name)]; !dup {
				mg.byName[p.key(d, name.Name)] = i
			}
		}
	}
	return mg
}

func (mg *merger) decl(d ast.Decl) {
	switch d := d.(type) {
	case *ast.ExtendDecl:
		if d.IsScreen() {
			mg.extendScreen(d)
		} else {
			mg.extendType(d)
		}
	case *ast.ExcludeDecl:
		i, ok := mg.tests[d.Test.Value]
		if !ok {
			mg.bag.Add(diag.E1007, rangeOf(d.Test), d.Test.Value)
			return
		}
		mg.excluded[i] = true
		mg.p.Excluded = append(mg.p.Excluded, d)
	default:
		mg.p.Decls = append(mg.p.Decls, d)
		mg.p.origin[d] = true
		for _, m := range members(d) {
			mg.p.origin[m] = true
		}
	}
}

var (
	kindType   = "entity or actor"
	kindScreen = "screen"
	kindPart   = "screen part"
	noteHere   = "declared here"
	noteKind   = "%q is declared here, but only entities and actors can be extended"
)

// base returns the base declaration named name in the bounded context of d.
func (mg *merger) base(d ast.Node, name string) ast.Decl {
	if i, ok := mg.byName[mg.p.key(d, name)]; ok {
		return mg.p.Decls[i]
	}
	return nil
}

func (mg *merger) extendType(d *ast.ExtendDecl) {
	key := mg.p.key(d, d.Name.Name)
	t := mg.types[key]
	if t == nil {
		switch b := mg.base(d, d.Name.Name).(type) {
		case *ast.EntityDecl:
			cp := *b
			cp.Members = append([]ast.Member(nil), b.Members...)
			t = &extType{decl: &cp, name: b.Name, members: &cp.Members}
		case *ast.ProjectionDecl:
			// The base model writes a projection row in full (RFC §18,
			// decision 21): it would drop the extension's fields.
			mg.bag.Add(diag.E1010, rangeOf(d.Name), d.Name.Name).Note(rangeOf(b.Name), noteHere)
			return
		case nil:
			mg.bag.Add(diag.E1002, rangeOf(d.Name), kindType, d.Name.Name)
			return
		default:
			mg.bag.Add(diag.E1002, rangeOf(d.Name), kindType, d.Name.Name).
				Note(rangeOf(declName(b)), noteKind, d.Name.Name)
			return
		}
		t.base = map[string]ast.Member{}
		for _, m := range *t.members {
			if name := memberName(m); name != "" {
				if _, dup := t.base[name]; !dup {
					t.base[name] = m
				}
			}
		}
		mg.types[key] = t
		mg.p.Decls[mg.byName[key]] = t.decl
	}
	for _, m := range d.Members {
		mg.member(t, m)
	}
}

func (mg *merger) member(t *extType, m ast.Member) {
	name := memberName(m)
	b := t.base[name]
	switch m := m.(type) {
	case *ast.OnDecl:
		// The event is checked in finish: it may be declared further down in the extension.
		t.ons = append(t.ons, m)
	case *ast.RuleDecl:
		baseRule, ok := b.(*ast.RuleDecl)
		switch {
		case b == nil:
			mg.bag.Add(diag.E1005, rangeOf(m.Name), t.name.Name, name)
			return
		case !ok:
			mg.bag.Add(diag.E1003, rangeOf(m.Name), name).Note(memberRange(b), noteHere)
			return
		case !sameSignature(baseRule, m):
			mg.bag.Add(diag.E1004, source.Range{Start: m.Rule, End: m.Result.End()}, name).
				Note(source.Range{Start: baseRule.Rule, End: baseRule.Result.End()}, "base rule")
			return
		}
		// An override takes the place of the base rule.
		for i, x := range *t.members {
			if memberName(x) == name {
				if _, isRule := x.(*ast.RuleDecl); isRule {
					(*t.members)[i] = m
					mg.p.origin[m] = true
					return
				}
			}
		}
		return
	default:
		if b != nil {
			mg.bag.Add(diag.E1003, memberRange(m), name).Note(memberRange(b), noteHere)
			return
		}
	}
	*t.members = append(*t.members, m)
	mg.p.origin[m] = true
}

// extendScreen adds the lines of an extension to the parts of a screen (RFC
// §21): "in Actions" goes into the only line of part Actions on the screen.
func (mg *merger) extendScreen(d *ast.ExtendDecl) {
	key := mg.p.key(d, d.Name.Name)
	s := mg.screens[key]
	if s == nil {
		b, ok := mg.base(d, d.Name.Name).(*ast.ScreenDecl)
		if !ok {
			mg.bag.Add(diag.E1002, rangeOf(d.Name), kindScreen, d.Name.Name)
			return
		}
		cp := *b
		s = &cp
		mg.screens[key] = s
		mg.p.Decls[mg.byName[key]] = s
	}
	for _, in := range d.Ins {
		for _, n := range in.Nodes {
			mg.p.origin[n] = true
		}
		nodes, found := addToPart(s.Nodes, in.Part.Name, in.Nodes)
		switch {
		case found == 0:
			mg.bag.Add(diag.E1002, rangeOf(in.Part), kindPart, d.Name.Name+"."+in.Part.Name)
		case found > 1:
			mg.bag.Add(diag.E1011, rangeOf(in.Part), d.Name.Name, in.Part.Name, found)
		default:
			s.Nodes = nodes
		}
	}
}

// addToPart returns lines with the children of part lines added (copies
// along the path to them) and the number of part lines found.
func addToPart(nodes []ast.UINode, part string, add []ast.UINode) ([]ast.UINode, int) {
	out, found := nodes, 0
	set := func(i int, n ast.UINode) {
		if &out[0] == &nodes[0] {
			out = append([]ast.UINode(nil), nodes...)
		}
		out[i] = n
	}
	for i, n := range nodes {
		switch n := n.(type) {
		case *ast.PrimNode:
			if n.Name.Name == part {
				cp := *n
				cp.Children = append(append([]ast.UINode(nil), n.Children...), add...)
				set(i, &cp)
				found++
				continue
			}
			if kids, k := addToPart(n.Children, part, add); k > 0 {
				cp := *n
				cp.Children = kids
				set(i, &cp)
				found += k
			}
		case *ast.FieldNode:
			if kids, k := addToPart(n.Children, part, add); k > 0 {
				cp := *n
				cp.Children = kids
				set(i, &cp)
				found += k
			}
		}
	}
	return out, found
}

// finish checks reactions and removes excluded tests.
func (mg *merger) finish() {
	for _, t := range mg.types {
		events := map[string]bool{}
		for _, m := range *t.members {
			if e, ok := m.(*ast.EventDecl); ok {
				events[e.Name.Name] = true
			}
		}
		bad := map[ast.Member]bool{}
		for _, on := range t.ons {
			if len(on.Path) == 0 && !events[on.Event.Name] { // sema checks an event of another bounded context
				mg.bag.Add(diag.E1006, rangeOf(on.Event), t.name.Name, on.Event.Name)
				bad[on] = true
			}
		}
		if len(bad) > 0 {
			kept := (*t.members)[:0]
			for _, m := range *t.members {
				if !bad[m] {
					kept = append(kept, m)
				}
			}
			*t.members = kept
		}
	}
	if len(mg.excluded) > 0 {
		decls := mg.p.Decls[:0:0]
		for i, d := range mg.p.Decls {
			if !mg.excluded[i] {
				decls = append(decls, d)
			}
		}
		mg.p.Decls = decls
	}
	diag.Sort(mg.bag.List())
}

func sameSignature(a, b *ast.RuleDecl) bool {
	if len(a.Params.List) != len(b.Params.List) || ast.TypeString(a.Result) != ast.TypeString(b.Result) {
		return false
	}
	for i, pa := range a.Params.List {
		pb := b.Params.List[i]
		if pa.Name.Name != pb.Name.Name || ast.TypeString(pa.Type) != ast.TypeString(pb.Type) {
			return false
		}
	}
	return true
}

func rangeOf(n ast.Node) source.Range { return source.Range{Start: n.Pos(), End: n.End()} }

// memberRange is the range of a member heading: the name, not the whole body.
func memberRange(m ast.Member) source.Range {
	switch m := m.(type) {
	case *ast.IDDecl:
		return source.Range{Start: m.ID, End: m.ID + source.Pos(len("id"))}
	case *ast.OnDecl:
		return rangeOf(m.Event)
	}
	if name := memberIdent(m); name != nil {
		return rangeOf(name)
	}
	return rangeOf(m)
}

// memberName returns the name of a member: "id" for id, "" for on and check.
func memberName(m ast.Member) string {
	if _, ok := m.(*ast.IDDecl); ok {
		return "id"
	}
	if id := memberIdent(m); id != nil {
		return id.Name
	}
	return ""
}

func memberIdent(m ast.Member) *ast.Ident {
	switch m := m.(type) {
	case *ast.FieldDecl:
		return m.Name
	case *ast.IndexDecl:
		return m.Name
	case *ast.CommandDecl:
		return m.Name
	case *ast.EventDecl:
		return m.Name
	case *ast.NotificationDecl:
		return m.Name
	case *ast.ErrorDecl:
		return m.Name
	case *ast.QueryDecl:
		return m.Name
	case *ast.RuleDecl:
		return m.Name
	case *ast.MethodDecl:
		return m.Name
	case *ast.RequestDecl:
		return m.Name
	}
	return nil
}

// declName returns the name of a declaration or nil (tests, let, extend, exclude).
func declName(d ast.Decl) *ast.Ident {
	switch d := d.(type) {
	case *ast.RoleDecl:
		return d.Name
	case *ast.FeatureDecl:
		return d.Name
	case *ast.EnumDecl:
		return d.Name
	case *ast.ValueDecl:
		return d.Name
	case *ast.EntityDecl:
		return d.Name
	case *ast.ProjectionDecl:
		return d.Name
	case *ast.ExternalDecl:
		return d.Name
	case *ast.AppDecl:
		return d.Name
	case *ast.ScreenDecl:
		return d.Name
	case *ast.PrimitiveDecl:
		return d.Name
	case *ast.MigrationDecl:
		return d.Name
	case *ast.ACLDecl:
		return d.Source
	}
	return nil
}

// members returns the members or items of a declaration.
func members(d ast.Decl) []ast.Node {
	var out []ast.Node
	switch d := d.(type) {
	case *ast.EntityDecl:
		for _, m := range d.Members {
			out = append(out, m)
		}
	case *ast.ProjectionDecl:
		for _, m := range d.Members {
			out = append(out, m)
		}
	case *ast.ValueDecl:
		for _, m := range d.Members {
			out = append(out, m)
		}
	case *ast.ExternalDecl:
		for _, m := range d.Members {
			out = append(out, m)
		}
	}
	return out
}
