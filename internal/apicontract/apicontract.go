// Package apicontract is the contract of a model's external API
// (RFC §31.3.1): the module's api/ files, published methods, version
// snapshots. The contract does not change the model: what is published and
// in which versions lives next to it, and an API change is reviewed as a
// diff of these files.
//
//	api/contract.json         — published methods (patterns) and deprecations
//	api/versions/<date>.json  — snapshots of released versions (reasonr api release)
package apicontract

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/model"
)

// Dir is the contract directory in a module.
const Dir = "api"

// Contract — api/contract.json.
type Contract struct {
	Version int `json:"version"`
	// Publish lists the published methods: "Ordering.Order.Cancel" or the
	// pattern "Ordering.Order.*" (all public commands and queries of the type).
	Publish []string `json:"publish"`
	// Deprecate lists deprecated methods with the date after which they may be removed.
	Deprecate []model.APIDeprecation `json:"deprecate,omitempty"`
}

// Source holds the module's api/ files.
type Source struct {
	Contract Contract
	Versions []model.APIVersion // by date
}

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// Load reads the module's api/ in fsys; nil when there is no directory.
func Load(fsys fs.FS) (*Source, error) {
	data, err := fs.ReadFile(fsys, path.Join(Dir, "contract.json"))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	src := &Source{}
	if err := json.Unmarshal(data, &src.Contract); err != nil {
		return nil, fmt.Errorf("%s/contract.json: %w", Dir, err)
	}
	entries, err := fs.ReadDir(fsys, path.Join(Dir, "versions"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	for _, e := range entries {
		date, ok := strings.CutSuffix(e.Name(), ".json")
		if e.IsDir() || !ok || !dateRe.MatchString(date) {
			continue
		}
		data, err := fs.ReadFile(fsys, path.Join(Dir, "versions", e.Name()))
		if err != nil {
			return nil, err
		}
		var v model.APIVersion
		if err := json.Unmarshal(data, &v); err != nil {
			return nil, fmt.Errorf("%s/versions/%s: %w", Dir, e.Name(), err)
		}
		v.Date = date
		src.Versions = append(src.Versions, v)
	}
	sort.Slice(src.Versions, func(i, j int) bool { return src.Versions[i].Date < src.Versions[j].Date })
	return src, nil
}

// Resolve expands publication patterns against schema s and returns the
// contract for the schema; unknown lists publication entries that name no
// public method of the model.
func Resolve(src *Source, s *model.Schema) (api *model.API, unknown []string) {
	if src == nil {
		return nil, nil
	}
	api = &model.API{Deprecated: src.Contract.Deprecate, Versions: src.Versions}
	for _, p := range src.Contract.Publish {
		found := false
		for _, full := range Public(s) {
			if Matches(p, full) {
				found = true
				if !slices.Contains(api.Methods, full) {
					api.Methods = append(api.Methods, full)
				}
			}
		}
		if !found {
			unknown = append(unknown, p)
		}
	}
	sort.Strings(api.Methods)
	return api, unknown
}

// Matches reports whether publication entry p names method full:
// "Type.method" or "Type.*".
func Matches(p, full string) bool {
	if typ, ok := strings.CutSuffix(p, ".*"); ok {
		return strings.HasPrefix(full, typ+".") && !strings.Contains(full[len(typ)+1:], ".")
	}
	return p == full
}

// Public returns the public commands and queries of a schema: "Type.method".
func Public(s *model.Schema) []string {
	var out []string
	for i := range s.Types {
		ti := &s.Types[i]
		for j := range ti.Methods {
			mi := &ti.Methods[j]
			if mi.Public && (mi.Kind == model.MethodCommand || mi.Kind == model.MethodQuery) {
				out = append(out, ti.Name+"."+mi.Name)
			}
		}
	}
	sort.Strings(out)
	return out
}

// Snapshot builds a snapshot of the published API of schema s on date date:
// the methods of contract api and the value objects and enums their
// parameters and results reach.
func Snapshot(s *model.Schema, api *model.API, date string) model.APIVersion {
	v := model.APIVersion{Date: date}
	types := map[model.TypeRef]bool{}
	var reach func(t model.Type)
	reach = func(t model.Type) {
		switch t.Kind {
		case model.KindRecord, model.KindEnum:
			ti := s.Type(t.Ref)
			if ti == nil || types[t.Ref] || ti.Kind != model.TypeValue && ti.Kind != model.TypeEnum {
				return
			}
			types[t.Ref] = true
			for _, f := range ti.Fields {
				reach(f.Type)
			}
		}
		if t.Elem != nil {
			reach(*t.Elem)
		}
		if t.Key != nil {
			reach(*t.Key)
		}
	}
	for _, full := range api.Methods {
		ti, mi := method(s, full)
		if mi == nil {
			continue
		}
		m := model.APIMethod{Type: ti.Name, Name: mi.Name, Kind: "command", Create: mi.Create}
		if mi.Kind == model.MethodQuery {
			m.Kind = "query"
		}
		for _, p := range mi.Params {
			m.Params = append(m.Params, model.APIField{Name: p.Name, Type: s.TypeText(p.Type), Default: p.Default != nil})
			reach(p.Type)
		}
		if mi.Result != nil {
			m.Result = s.TypeText(*mi.Result)
			reach(*mi.Result)
		}
		v.Methods = append(v.Methods, m)
	}
	for i := range s.Types {
		ti := &s.Types[i]
		if !types[ti.Ref] {
			continue
		}
		t := model.APIType{Name: ti.Name, Values: ti.Enum}
		for _, f := range ti.Fields {
			t.Fields = append(t.Fields, model.APIField{Name: f.Name, Type: s.TypeText(f.Type), Default: f.Default != nil})
		}
		v.Types = append(v.Types, t)
	}
	return v
}

// method returns the type and the method "Type.method" of a schema.
func method(s *model.Schema, full string) (*model.TypeInfo, *model.MethodInfo) {
	i := strings.LastIndex(full, ".")
	if i < 0 {
		return nil, nil
	}
	ti := s.Lookup(full[:i])
	if ti == nil || ti.Name != full[:i] {
		return nil, nil
	}
	return ti, ti.Method(full[i+1:])
}

// Current returns the method of the current schema that matches method m of
// a version: with the same name or renamed (was).
func Current(s *model.Schema, m *model.APIMethod) (*model.TypeInfo, *model.MethodInfo) {
	ti := s.Lookup(m.Type)
	if ti == nil || ti.Name != m.Type {
		return nil, nil
	}
	if mi := ti.Method(m.Name); mi != nil {
		return ti, mi
	}
	for j := range ti.Methods {
		if ti.Methods[j].Was == m.Name {
			return ti, &ti.Methods[j]
		}
	}
	return ti, nil
}

// Param returns the parameter of current method mi that matches version
// parameter name: with the same name or renamed (was).
func Param(mi *model.MethodInfo, name string) *model.FieldInfo {
	for i := range mi.Params {
		if mi.Params[i].Name == name {
			return &mi.Params[i]
		}
	}
	for i := range mi.Params {
		if mi.Params[i].WasName == name {
			return &mi.Params[i]
		}
	}
	return nil
}

// View returns schema s as clients of version v see it (RFC §31.3.1):
// the same types, fields and methods with the same numbers,
// but names as in the version. A renamed method, parameter, value object
// field or enum value (was) keeps its old name if the version knew it by
// that name. A codec over this schema reads arguments and writes results
// with the old names, so no translation code is needed. The published
// methods are those of the version.
func View(s *model.Schema, v *model.APIVersion) *model.Schema {
	out := *s
	out.Types = make([]model.TypeInfo, len(s.Types))
	copy(out.Types, s.Types)
	api := model.API{Versions: s.API.Versions, Deprecated: s.API.Deprecated}
	for i := range out.Types {
		ti := &out.Types[i]
		if vt := v.Type(ti.Name); vt != nil {
			ti.Fields = renameFields(ti.Fields, func(name string) bool {
				for _, f := range vt.Fields {
					if f.Name == name {
						return true
					}
				}
				return false
			})
			if ti.EnumWas != nil {
				ti.Enum = append([]string(nil), ti.Enum...)
				for k, was := range ti.EnumWas {
					if was != "" && slices.Contains(vt.Values, was) && !slices.Contains(vt.Values, ti.Enum[k]) {
						ti.Enum[k] = was
					}
				}
			}
		}
		methods := make([]model.MethodInfo, len(ti.Methods))
		copy(methods, ti.Methods)
		ti.Methods = methods
		for j := range ti.Methods {
			mi := &ti.Methods[j]
			vm := v.Method(ti.Name, mi.Name)
			if vm == nil && mi.Was != "" {
				if vm = v.Method(ti.Name, mi.Was); vm != nil {
					mi.Name = mi.Was
				}
			}
			if vm == nil {
				continue
			}
			api.Methods = append(api.Methods, vm.FullName())
			mi.Params = renameFields(mi.Params, func(name string) bool {
				for _, p := range vm.Params {
					if p.Name == name {
						return true
					}
				}
				return false
			})
		}
	}
	sort.Strings(api.Methods)
	out.API = &api
	return &out
}

// renameFields returns a copy of the fields in which a field with an old
// name (WasName) known to the version (inVersion) is called by it, if the
// version does not know the new name.
func renameFields(fs []model.FieldInfo, inVersion func(string) bool) []model.FieldInfo {
	out := make([]model.FieldInfo, len(fs))
	copy(out, fs)
	for i := range out {
		f := &out[i]
		if f.WasName != "" && !inVersion(f.Name) && inVersion(f.WasName) {
			f.Name = f.WasName
		}
	}
	return out
}
