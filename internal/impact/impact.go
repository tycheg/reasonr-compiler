// Package impact is test impact analysis (RFC §27.4): which
// acceptance tests a model change may affect. Nodes are IR functions,
// schema types and event reaction tables; each has a hash of its canonical
// form (without source positions and without numbers the schema assigns).
// A test is affected if a node reachable from it is changed or new.
package impact

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ir"
	"github.com/tycheg/reasonr-compiler/internal/model"
)

// Graph is the model's nodes with hashes and test reachability.
type Graph struct {
	Hash  map[string]string   `json:"hash"`  // node → hash
	Tests map[string][]string `json:"tests"` // test → reachable nodes, alphabetically
	// Flags are the model's feature flags; TestFlags are the flags a test turns on.
	Flags     []string            `json:"flags,omitempty"`
	TestFlags map[string][]string `json:"testFlags,omitempty"`
}

// UI is the node of the UI program (compiler.Graph adds its hash).
const UI = "ui"

// Nodes: "func <kind> <name>", "type <Type>", "reactions <Type.event>".
func funcNode(f *ir.Func) string { return "func " + f.Kind.String() + " " + f.Name }

// Build builds the model graph from its IR and schema.
func Build(p *ir.Program, s *model.Schema) *Graph {
	g := &Graph{Hash: map[string]string{}, Tests: map[string][]string{}, TestFlags: map[string][]string{}}
	byName := map[string][]*ir.Func{}
	for _, f := range p.Funcs {
		g.Hash[funcNode(f)] = Hash((&ir.Program{Funcs: []*ir.Func{f}}).String())
		byName[f.Name] = append(byName[f.Name], f)
	}
	types := map[string]bool{}
	for i := range s.Types {
		ti := &s.Types[i]
		types[ti.Name] = true
		g.Hash["type "+ti.Name] = Hash(canonical(s, ti))
	}
	reactions := map[string][]string{}
	for _, r := range p.Reactions {
		names := append([]string(nil), r.Funcs...)
		sort.Strings(names)
		reactions[r.Event] = names
		g.Hash["reactions "+r.Event] = Hash(strings.Join(names, "\n"))
	}
	for _, f := range s.Features {
		g.Flags = append(g.Flags, f.Name)
	}
	for _, f := range p.Funcs {
		if f.Kind != ir.FuncTest || strings.Contains(f.Name, "\x00") { // performance test checks
			continue
		}
		g.Tests[f.Name] = reach(f, byName, types, reactions)
		for _, b := range f.Blocks {
			for _, in := range b.Instrs {
				if in.Op == ir.OpTestFeature {
					g.TestFlags[f.Name] = append(g.TestFlags[f.Name], in.Sym)
				}
			}
		}
	}
	return g
}

// reach is the set of nodes reachable from a test function: functions by
// instruction symbols (commands, events and their application, queries,
// rules, methods, check, timers, external system requests and fakes), the
// types they name, and reactions to their events (ACL included). A timer
// command is reachable only if time passes in the test (time offset):
// otherwise the timer does not fire. A request implementation in a fake is
// reachable only if the test calls fake commands (Setup): until the fake is
// created, requests are only recorded.
func reach(test *ir.Func, byName map[string][]*ir.Func, types map[string]bool, reactions map[string][]string) []string {
	clock, called := false, map[string]bool{}
	for _, b := range test.Blocks {
		for _, in := range b.Instrs {
			switch in.Op {
			case ir.OpTestTime:
				clock = true
			case ir.OpTestCmd, ir.OpTestQuery:
				if i := strings.LastIndex(in.Sym, "."); i > 0 {
					called[in.Sym[:i]] = true
				}
			}
		}
	}
	seen := map[string]bool{}
	var queue []*ir.Func
	visitFunc := func(f *ir.Func) {
		if n := funcNode(f); !seen[n] {
			seen[n] = true
			queue = append(queue, f)
		}
	}
	// visitType: the type of a symbol is its longest prefix that names a schema type.
	visitType := func(sym string) {
		for t := sym; t != ""; {
			if types[t] {
				seen["type "+t] = true
				for _, f := range byName[t+".check"] {
					visitFunc(f)
				}
				return
			}
			i := strings.LastIndex(t, ".")
			if i < 0 {
				return
			}
			t = t[:i]
		}
	}
	visitSym := func(sym string) {
		if sym == "" {
			return
		}
		for _, f := range byName[sym] {
			visitFunc(f)
		}
		for _, f := range byName[sym+".check"] {
			visitFunc(f)
		}
		visitType(sym)
		if fs, ok := reactions[sym]; ok {
			seen["reactions "+sym] = true
			for _, name := range fs {
				for _, f := range byName[name] {
					visitFunc(f)
				}
			}
		}
	}
	visitFunc(test)
	for len(queue) > 0 {
		f := queue[0]
		queue = queue[1:]
		if f.Owner != "" {
			visitSym(f.Owner)
		}
		for _, b := range f.Blocks {
			for _, in := range b.Instrs {
				switch in.Op {
				case ir.OpSchedule:
					if !clock {
						visitType(in.Sym)
						continue
					}
				case ir.OpRequest:
					if i := strings.LastIndex(in.Sym, "."); i > 0 && !called[in.Sym[:i]] {
						visitType(in.Sym)
						continue
					}
				}
				visitSym(in.Sym)
				if in.Find != nil {
					visitSym(in.Find.Type)
				}
				for _, sub := range in.Subjects {
					visitSym(sub.Type)
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for n := range seen {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// canonical is the form of a schema type without numbers: fields, indexes,
// methods with signatures and access, enum values, id strategy — by name.
func canonical(s *model.Schema, ti *model.TypeInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s id=%d", ti.Kind, ti.Name, ti.ID)
	if t := s.Type(ti.IDType); t != nil {
		fmt.Fprintf(&b, " idType=%s", t.Name)
	}
	if t := s.Type(ti.Owner); t != nil {
		fmt.Fprintf(&b, " owner=%s", t.Name)
	}
	fmt.Fprintf(&b, " role=%s enum=%v enumWas=%v\n", ti.Role, ti.Enum, ti.EnumWas)
	field := func(f *model.FieldInfo) string {
		out := f.Name + " " + s.TypeText(f.Type) + " was=" + f.WasName
		if f.Default != nil {
			out += " default=" + f.Default.Text
		}
		if f.Personal != nil {
			out += fmt.Sprintf(" personal=%v", f.Personal.Subject)
		}
		return out
	}
	for i := range ti.Fields {
		fmt.Fprintf(&b, "field %s\n", field(&ti.Fields[i]))
	}
	for _, ix := range ti.Indexes {
		fmt.Fprintf(&b, "index %s %v unique=%v error=%s\n", ix.Name, ix.Keys, ix.Unique, ix.Error)
	}
	for i := range ti.Methods {
		m := &ti.Methods[i]
		fmt.Fprintf(&b, "method %d %s public=%v create=%v was=%s", m.Kind, m.Name, m.Public, m.Create, m.Was)
		for _, a := range m.Access {
			actor := ""
			if t := s.Type(a.Actor); t != nil {
				actor = t.Name
			}
			fmt.Fprintf(&b, " access(%d %s %s %v)", a.Kind, a.Role, actor, a.Field)
		}
		for k := range m.Params {
			fmt.Fprintf(&b, " (%s)", field(&m.Params[k]))
		}
		if m.Result != nil {
			fmt.Fprintf(&b, " → %s", s.TypeText(*m.Result))
		}
		fmt.Fprintf(&b, " acl=%v\n", m.ACL)
	}
	for _, c := range ti.Callers {
		if t := s.Type(c); t != nil {
			fmt.Fprintf(&b, "caller %s\n", t.Name)
		}
	}
	return b.String()
}

// Hash is the hash of a node's canonical form.
func Hash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:8])
}

// Affected lists the affected tests of one model and the reasons.
type Affected struct {
	Model string `json:"model"` // "" is the base model
	Total int    `json:"total"`
	// UI: the UI program changed; with --driver web every test is affected.
	UI      bool                `json:"ui"`
	Tests   map[string][]string `json:"tests"` // test → changed nodes (empty for a new test)
	Skipped []string            `json:"skipped"`
}

// Compare compares the graph of the new version with the base graph: a test
// is affected if the base lacks it or one of its nodes is changed or new.
func Compare(model string, base, cur *Graph) *Affected {
	a := &Affected{Model: model, Total: len(cur.Tests), Tests: map[string][]string{}, Skipped: []string{},
		UI: base.Hash[UI] != cur.Hash[UI]}
	names := make([]string, 0, len(cur.Tests))
	for name := range cur.Tests {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if _, ok := base.Tests[name]; !ok {
			a.Tests[name] = []string{"new test"}
			continue
		}
		var changed []string
		for _, n := range cur.Tests[name] {
			if base.Hash[n] != cur.Hash[n] {
				changed = append(changed, n)
			}
		}
		if len(changed) > 0 {
			a.Tests[name] = changed
		} else {
			a.Skipped = append(a.Skipped, name)
		}
	}
	return a
}

// FlagViolations checks old tests when a flag is introduced (RFC §23.4,
// §27.4): if a change introduces a flag, a test whose
// function changed must turn the new flag on. A commit that removes a flag
// may change old tests. allowed lists explicit exceptions.
func FlagViolations(base, cur *Graph, allowed map[string]bool) []string {
	var added, removed []string
	for _, f := range cur.Flags {
		if !contains(base.Flags, f) {
			added = append(added, f)
		}
	}
	for _, f := range base.Flags {
		if !contains(cur.Flags, f) {
			removed = append(removed, f)
		}
	}
	if len(added) == 0 || len(removed) > 0 {
		return nil
	}
	var out []string
	for name, nodes := range cur.Tests {
		if _, old := base.Tests[name]; !old || allowed[name] {
			continue
		}
		self := ""
		for _, n := range nodes {
			if strings.HasPrefix(n, "func test ") && strings.TrimPrefix(n, "func test ") == name {
				self = n
			}
		}
		if self == "" || base.Hash[self] == cur.Hash[self] {
			continue
		}
		enables := false
		for _, f := range cur.TestFlags[name] {
			enables = enables || contains(added, f)
		}
		if !enables {
			out = append(out, fmt.Sprintf("test %q changed while the change introduces flag %s: old tests check behavior without the flag — cover the new behavior with a test that has a feature line", name, strings.Join(added, ", ")))
		}
	}
	sort.Strings(out)
	return out
}

func contains(xs []string, x string) bool { return slices.Contains(xs, x) }
