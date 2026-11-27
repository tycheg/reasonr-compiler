// Package compat compares the schema of a previous model version with the
// new model (RFC §32): reasonr check
// --against. Matching goes by name: schema numbers change between versions.
package compat

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/schemagen"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

var (
	textKindChanged  = "%s became %s"
	textIDChanged    = "the id changed"
	textEnumRemoved  = "value %s was removed"
	textTypeChanged  = "type %s became %s"
	textField        = "field %s"
	textEventRemoved = "removed or renamed"
	textArgRemoved   = "argument %s was removed"
	textArgType      = "the type of argument %s %s became %s"
	textArgNew       = "new argument %s has no default value"
)

func msg(t string, args ...any) diag.Message { return diag.Message{Format: t, Args: args} }

// Check compares the old schema old with the new model and returns E11xx
// and W11xx diagnostics. The new model must have no errors.
func Check(old *model.Schema, p *module.Program, info *sema.Info) []*diag.Diagnostic {
	return run(&checker{old: old, new: schemagen.Generate(p, info), p: p, info: info})
}

// CheckSchema compares the old schema old with the schema new of a model
// built without its sources at hand (a model file): the same diagnostics
// without places.
func CheckSchema(old, new *model.Schema) []*diag.Diagnostic {
	return run(&checker{old: old, new: new})
}

func run(c *checker) []*diag.Diagnostic {
	old := c.old
	// A change that needs a manual migration is allowed if the new version
	// has a migration.
	for _, m := range c.new.Migrations {
		if !slices.Contains(old.Migrations, m) {
			c.migrated = true
		}
	}
	for i := range old.Types {
		c.typ(&old.Types[i])
	}
	list := c.bag.List()
	diag.Sort(list)
	return list
}

type checker struct {
	old, new *model.Schema
	p        *module.Program
	info     *sema.Info
	bag      diag.Bag
	migrated bool
}

func (c *checker) typ(ot *model.TypeInfo) {
	nt := c.new.Lookup(ot.Name)
	stored := ot.Kind == model.TypeEntity || ot.Kind == model.TypeActor || ot.Kind == model.TypeProjection
	if nt == nil {
		if stored {
			c.bag.Add(diag.W1102, c.fallback(), fmt.Sprintf("%s %s", ot.Kind, ot.Name))
		}
		return
	}
	var d *types.Decl
	if c.info != nil {
		d = c.info.Decls[nt.Name]
	}
	at := declAt(d)
	switch {
	case nt.Kind != ot.Kind && (stored || ot.Kind == model.TypeValue || ot.Kind == model.TypeEnum):
		c.manual(at, ot.Name, msg(textKindChanged, ot.Kind.String(), nt.Kind.String()))
		return
	case stored && (nt.ID != ot.ID || c.refName(c.new, nt.IDType) != c.refName(c.old, ot.IDType)):
		c.manual(at, ot.Name, textIDChanged)
	case ot.Kind == model.TypeExternal:
		// An external system stores no model data: fields, events and
		// commands exist only in a fake of the test program (RFC §18).
		return
	}
	if stored || ot.Kind == model.TypeValue {
		c.fields(ot, nt, d)
	}
	if ot.Kind == model.TypeEnum {
		for _, v := range ot.Enum {
			if !slices.Contains(nt.Enum, v) {
				c.manual(at, ot.Name, msg(textEnumRemoved, v))
			}
		}
	}
	for _, ix := range nt.Indexes {
		if !ix.Unique {
			continue
		}
		if old := ot.Index(ix.Name); old == nil || !old.Unique {
			at := source.Range{}
			if d != nil {
				at = rangeOf(d.Indexes[ix.Ref].Node.Name)
			}
			c.bag.Add(diag.W1103, at, nt.Name+"."+ix.Name)
		}
	}
	for i := range ot.Methods {
		c.method(&ot.Methods[i], nt, d)
	}
}

// fields compares fields; a field renamed with was is looked up by its old name.
func (c *checker) fields(ot, nt *model.TypeInfo, d *types.Decl) {
	for _, of := range ot.Fields {
		nf := nt.Field(of.Name)
		if nf == nil {
			for i := range nt.Fields {
				if nt.Fields[i].WasName == of.Name {
					nf = &nt.Fields[i]
				}
			}
		}
		if nf == nil {
			c.bag.Add(diag.W1102, declAt(d), msg(textField, ot.Name+"."+of.Name))
			continue
		}
		at := declAt(d)
		if d != nil && int(nf.Ref) < len(d.Fields) { // fields of an inner entity of the aggregate come after its own
			at = rangeOf(d.Fields[nf.Ref].Node.(*ast.FieldDecl).Name)
		}
		name := ot.Name + "." + nf.Name
		ots, nts := c.describe(c.old, of.Type), c.describe(c.new, nf.Type)
		switch {
		case !widens(strings.TrimSuffix(ots, "?"), strings.TrimSuffix(nts, "?")):
			c.manual(at, msg(textField, name), msg(textTypeChanged, ots, nts))
		case of.Type.Optional && !nf.Type.Optional:
			c.bag.Add(diag.E1103, at, name, ots, nts)
		}
	}
}

func (c *checker) method(om *model.MethodInfo, nt *model.TypeInfo, d *types.Decl) {
	nm := nt.Method(om.Name)
	if nm != nil && nm.Kind != om.Kind {
		nm = nil
	}
	switch om.Kind {
	case model.MethodCommand:
		if nm == nil {
			c.bag.Add(diag.W1101, declAt(d), nt.Name+"."+om.Name)
		}
	case model.MethodEvent:
		name := nt.Name + "." + om.Name
		if nm == nil {
			c.bag.Add(diag.E1102, declAt(d), name, textEventRemoved)
			return
		}
		// An event of an inner entity "Stop.arrived" is declared in it; its
		// first parameter is the inner entity id (docs/plans/0011, §3.5).
		// Without the sources (d == nil) the diagnostics have no places.
		var m *types.Method
		off := 0
		if d != nil {
			m = d.Method(nm.Name)
			if nm.Inner != 0 {
				short, ev, _ := strings.Cut(nm.Name, ".")
				if in := d.InnerNamed(short); in != nil {
					m, off = in.Method(ev), 1
				}
			}
			if m == nil {
				return
			}
		}
		at := source.Range{}
		if m != nil {
			at = rangeOf(memberName(m.Node))
		}
		paramAt := func(np *model.FieldInfo) source.Range {
			if i := int(np.Ref) - off; m != nil && i >= 0 && i < len(m.Params) {
				return rangeOf(m.Params[i].Node.(*ast.Param).Name)
			}
			return at
		}
		for _, op := range om.Params {
			np := paramOf(nm, op.Name)
			if np == nil {
				c.bag.Add(diag.E1102, at, name, msg(textArgRemoved, op.Name))
				continue
			}
			ots, nts := c.describe(c.old, op.Type), c.describe(c.new, np.Type)
			if !widens(ots, nts) && !widens(ots+"?", nts) {
				c.bag.Add(diag.E1102, paramAt(np), name, msg(textArgType, op.Name, ots, nts))
			}
		}
		for _, np := range nm.Params {
			if paramOf(om, np.Name) == nil && np.Default == nil && !np.Type.Optional {
				c.bag.Add(diag.E1102, paramAt(&np), name, msg(textArgNew, np.Name))
			}
		}
	}
}

// typeTokens splits a type into names and signs: list<int> → list, <, int, >.
var typeTokens = regexp.MustCompile(`[\pL\pN_.]+|.`)

// widens reports that type nts reads stored values of type ots without a
// migration: the types match except for int that became bigint (the
// integer encoding is the same, the range is wider).
func widens(ots, nts string) bool {
	if ots == nts {
		return true
	}
	o, n := typeTokens.FindAllString(ots, -1), typeTokens.FindAllString(nts, -1)
	if len(o) != len(n) {
		return false
	}
	for i := range o {
		if o[i] != n[i] && !(o[i] == "int" && n[i] == "bigint") {
			return false
		}
	}
	return true
}

func paramOf(m *model.MethodInfo, name string) *model.FieldInfo {
	for i := range m.Params {
		if m.Params[i].Name == name {
			return &m.Params[i]
		}
	}
	return nil
}

// manual: a change that needs a manual migration (E1101) if there is none.
func (c *checker) manual(at source.Range, what, change any) {
	if !c.migrated {
		c.bag.Add(diag.E1101, at, what, change)
	}
}

// describe writes a type by names: list<Driver.Id>?.
func (c *checker) describe(s *model.Schema, t model.Type) string {
	var out string
	switch t.Kind {
	case model.KindEnum, model.KindRecord:
		out = c.refName(s, t.Ref)
	case model.KindID:
		out = c.refName(s, t.Ref) + ".Id"
	case model.KindList, model.KindSet, model.KindPage:
		out = t.Kind.String() + "<" + c.describe(s, *t.Elem) + ">"
	case model.KindMap:
		out = "map<" + c.describe(s, *t.Key) + ", " + c.describe(s, *t.Elem) + ">"
	default:
		out = t.Kind.String()
	}
	if t.Optional {
		out += "?"
	}
	return out
}

func (c *checker) refName(s *model.Schema, ref model.TypeRef) string {
	if t := s.Type(ref); t != nil {
		return t.Name
	}
	return ""
}

// fallback is the place for changes absent from the new model: the start of
// the first file of the module.
func (c *checker) fallback() source.Range {
	if c.p == nil {
		return source.Range{}
	}
	for _, f := range c.p.Files {
		return source.Range{Start: f.AST.FileStart, End: f.AST.FileStart}
	}
	return source.Range{}
}

func rangeOf(n ast.Node) source.Range { return source.Range{Start: n.Pos(), End: n.End()} }

// declAt is the place of the name of declaration d; none without the sources.
func declAt(d *types.Decl) source.Range {
	if d == nil {
		return source.Range{}
	}
	return rangeOf(declName(d))
}

func declName(d *types.Decl) *ast.Ident {
	switch n := d.Node.(type) {
	case *ast.EntityDecl:
		return n.Name
	case *ast.ProjectionDecl:
		return n.Name
	case *ast.ValueDecl:
		return n.Name
	case *ast.EnumDecl:
		return n.Name
	case *ast.ExternalDecl:
		return n.Name
	}
	return nil
}

func memberName(m ast.Member) *ast.Ident {
	switch n := m.(type) {
	case *ast.CommandDecl:
		return n.Name
	case *ast.EventDecl:
		return n.Name
	case *ast.QueryDecl:
		return n.Name
	}
	return nil
}
