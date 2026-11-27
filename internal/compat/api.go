package compat

import (
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/apicontract"
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// API compares the model with every released version of the external API
// (RFC §31.3.1): what clients of a version see must remain, under
// its own or its old name (was), with defaults for new parameters. A
// deprecated method may be removed after its Sunset date (today ≥ sunset).
// src holds the module's api/ files, s the model schema (with the contract).
func API(src *apicontract.Source, s *model.Schema, info *sema.Info, today string) []*diag.Diagnostic {
	var bag diag.Bag
	if src == nil {
		return nil
	}
	_, unknown := apicontract.Resolve(src, s)
	for _, p := range unknown {
		bag.Add(diag.E1107, source.Range{}, p)
	}
	a := &apiChecker{s: s, info: info, bag: &bag}
	for i := range src.Versions {
		v := &src.Versions[i]
		for j := range v.Methods {
			m := &v.Methods[j]
			if dep := s.API.Deprecation(m.FullName()); dep != nil && dep.Sunset <= today {
				continue // deprecated and past its date: may be removed
			}
			a.method(v, m)
		}
	}
	return bag.List()
}

type apiChecker struct {
	s    *model.Schema
	info *sema.Info
	bag  *diag.Bag
}

func (a *apiChecker) method(v *model.APIVersion, m *model.APIMethod) {
	ti, mi := apicontract.Current(a.s, m)
	d := a.info.Decls[m.Type]
	what := map[string]string{"command": "command ", "query": "query "}[m.Kind] + m.FullName()
	if ti == nil || mi == nil || !mi.Public {
		at := source.Range{}
		if d != nil {
			at = rangeOf(declName(d))
		}
		a.bag.Add(diag.E1104, at, v.Date, what)
		return
	}
	var tm *types.Method
	if d != nil {
		tm = d.Method(mi.Name)
	}
	at := source.Range{}
	if tm != nil {
		at = rangeOf(memberName(tm.Node))
	}
	var removed []*model.APIField
	for k := range m.Params {
		op := &m.Params[k]
		np := apicontract.Param(mi, op.Name)
		if np == nil {
			removed = append(removed, op)
			continue
		}
		if nt := a.s.TypeText(np.Type); nt != op.Type && nt != op.Type+"?" {
			a.bag.Add(diag.E1106, a.paramAt(tm, np.Name, at), v.Date, "parameter "+op.Name+" ("+what+")", op.Type, nt)
		}
	}
	var added []*model.FieldInfo
	for k := range mi.Params {
		if np := &mi.Params[k]; !old(m, np) {
			added = append(added, np)
		}
	}
	rename := len(removed) == 1 && len(added) == 1 // looks like a rename: hint was
	for _, np := range added {
		if np.Default == nil && !np.Type.Optional && !rename {
			a.bag.Add(diag.E1105, a.paramAt(tm, np.Name, at), v.Date, what, np.Name)
		}
	}
	for _, op := range removed {
		dg := a.bag.Add(diag.E1104, at, v.Date, "parameter "+op.Name+" ("+what+")")
		if rename && tm != nil {
			if p := param(tm, added[0].Name); p != nil {
				dg.Fix(diag.Message{Format: "rename: %s %s was %s",
					Args: []any{p.Name.Name, a.s.TypeText(added[0].Type), op.Name}},
					diag.Edit{Range: source.Range{Start: p.End(), End: p.End()}, NewText: " was " + op.Name})
			}
		}
	}
	if m.Result != "" && mi.Result != nil {
		if nt := a.s.TypeText(*mi.Result); nt != m.Result {
			a.bag.Add(diag.E1106, at, v.Date, "the result ("+what+")", m.Result, nt)
		}
		a.result(v, *mi.Result, map[string]bool{})
	}
}

// result checks the value objects and enums of a result: the fields and
// values clients of the version see remain (under the old name, was).
func (a *apiChecker) result(v *model.APIVersion, t model.Type, seen map[string]bool) {
	if t.Elem != nil {
		a.result(v, *t.Elem, seen)
	}
	if t.Kind != model.KindRecord && t.Kind != model.KindEnum {
		return
	}
	ti := a.s.Type(t.Ref)
	if ti == nil || seen[ti.Name] {
		return
	}
	seen[ti.Name] = true
	vt := v.Type(ti.Name)
	if vt == nil {
		return
	}
	at := source.Range{}
	if d := a.info.Decls[ti.Name]; d != nil {
		at = rangeOf(declName(d))
	}
	for _, of := range vt.Fields {
		nf := ti.Field(of.Name)
		if nf == nil {
			for i := range ti.Fields {
				if ti.Fields[i].WasName == of.Name {
					nf = &ti.Fields[i]
				}
			}
		}
		if nf == nil {
			a.bag.Add(diag.E1104, at, v.Date, "field "+ti.Name+"."+of.Name)
			continue
		}
		if nt := a.s.TypeText(nf.Type); nt != of.Type && strings.TrimSuffix(of.Type, "?") != nt {
			a.bag.Add(diag.E1106, at, v.Date, "field "+ti.Name+"."+of.Name, of.Type, nt)
		}
		a.result(v, nf.Type, seen)
	}
	for _, ov := range vt.Values {
		found := false
		for i, nv := range ti.Enum {
			found = found || nv == ov || i < len(ti.EnumWas) && ti.EnumWas[i] == ov
		}
		if !found {
			a.bag.Add(diag.E1104, at, v.Date, "value "+ti.Name+"."+ov)
		}
	}
}

// old reports whether parameter np was in method m of the version (under its own or old name).
func old(m *model.APIMethod, np *model.FieldInfo) bool {
	for _, op := range m.Params {
		if op.Name == np.Name || op.Name == np.WasName && np.WasName != "" {
			return true
		}
	}
	return false
}

func param(m *types.Method, name string) *ast.Param {
	for _, p := range m.Params {
		if n, ok := p.Node.(*ast.Param); ok && p.Name == name {
			return n
		}
	}
	return nil
}

func (a *apiChecker) paramAt(m *types.Method, name string, def source.Range) source.Range {
	if m != nil {
		if p := param(m, name); p != nil {
			return rangeOf(p.Name)
		}
	}
	return def
}
