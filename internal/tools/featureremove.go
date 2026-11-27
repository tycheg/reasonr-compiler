package tools

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/format"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/token"
)

// Removing a rolled-out flag from the code (reasonr feature remove,
// RFC §23.4, decision 11). The flag is replaced with a value: "on"
// (by default) or "off" (off). Conditions that depend on it are
// simplified: the branch that always runs takes the place of the if, the
// other is removed; a layout node whose condition is true loses its "if",
// one whose condition is false is removed with its nested lines. The flag
// declaration and its "##" are removed, the flag is dropped from the
// feature lines of tests; with off, the tests that turned the flag on are
// removed.

// Removal is the result of removing a flag.
type Removal struct {
	Feature string            // the full name
	Files   map[string][]byte // path → new text (formatted)
	// Tests are the removed tests (off): they checked the behavior under the flag.
	Tests []string
}

// RemoveFeature removes flag name from the project files of models models
// (base and extensions; files of dependency modules do not change).
func RemoveFeature(models []*pipeline.Model, name string, off bool) (*Removal, error) {
	r := &Removal{Files: map[string][]byte{}}
	edits := map[*module.File]map[[2]int]string{} // file → [from, to) → text
	paths := map[*module.File]string{}
	for _, m := range models {
		full := featureName(m.Info, name)
		if full == "" {
			continue
		}
		if r.Feature != "" && r.Feature != full {
			return nil, fmt.Errorf("%q names different flags: %s and %s — give the full name", name, r.Feature, full)
		}
		r.Feature = full
		for _, f := range m.Program.Files {
			if f.Dep != "" {
				continue
			}
			rm := &remover{info: m.Info, full: full, on: !off, f: f, src: f.Src.Source()}
			rm.file(f.AST)
			if len(rm.edits) == 0 {
				continue
			}
			if edits[f] == nil {
				edits[f] = map[[2]int]string{}
			}
			for k, v := range rm.edits {
				edits[f][k] = v
			}
			paths[f] = filepath.Join(m.Module.Dir, f.Rel)
			for _, t := range rm.tests {
				if !contains(r.Tests, t) {
					r.Tests = append(r.Tests, t)
				}
			}
		}
	}
	if r.Feature == "" {
		return nil, fmt.Errorf("the model has no feature flag %q", name)
	}
	// The same base model files go into every model: the edits match, so
	// they are collected by key.
	byPath := map[string]*module.File{}
	for f, p := range paths {
		if prev := byPath[p]; prev != nil {
			for k, v := range edits[f] {
				edits[prev][k] = v
			}
			continue
		}
		byPath[p] = f
	}
	for p, f := range byPath {
		out, err := apply(f.Src.Source(), edits[f])
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p, err)
		}
		formatted, diags := format.Source(source.NewFileSet(), p, out)
		if len(diags) == 0 {
			out = formatted
		}
		r.Files[p] = out
	}
	sort.Strings(r.Tests)
	return r, nil
}

// featureName is the full name of a model flag: given full or short, if the short one is unique.
func featureName(info *sema.Info, name string) string {
	if info.Features[name] != nil {
		return name
	}
	found := ""
	for full, f := range info.Features {
		if f.Name.Name == name {
			if found != "" {
				return ""
			}
			found = full
		}
	}
	return found
}

// apply applies edits that do not overlap.
func apply(src []byte, edits map[[2]int]string) ([]byte, error) {
	keys := make([][2]int, 0, len(edits))
	for k := range edits {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i][0] < keys[j][0] })
	var b strings.Builder
	at := 0
	for _, k := range keys {
		if k[0] < at {
			return nil, fmt.Errorf("edits overlap at position %d", k[0])
		}
		b.Write(src[at:k[0]])
		b.WriteString(edits[k])
		at = k[1]
	}
	b.Write(src[at:])
	return []byte(b.String()), nil
}

// remover collects the edits of one file.
type remover struct {
	info  *sema.Info
	full  string
	on    bool
	f     *module.File
	src   []byte
	edits map[[2]int]string
	tests []string
}

func (r *remover) off(p source.Pos) int { return r.f.Src.Offset(p) }

// lineStart is the offset of the start of the line of position p.
func (r *remover) lineStart(p source.Pos) int {
	return r.off(r.f.Src.LineStart(r.f.Src.Line(p)))
}

// lineEnd is the offset after the line break of the line of position p (the
// end of a node: the position right after it).
func (r *remover) lineEnd(end source.Pos) int {
	o := r.off(end)
	for o < len(r.src) && r.src[o] != '\n' {
		o++
	}
	if o < len(r.src) {
		o++
	}
	return o
}

func (r *remover) text(n ast.Node) string { return string(r.src[r.off(n.Pos()):r.off(n.End())]) }

func (r *remover) edit(from, to int, text string) {
	if r.edits == nil {
		r.edits = map[[2]int]string{}
	}
	r.edits[[2]int{from, to}] = text
}

// isFlag reports whether an expression reads the removed flag.
func (r *remover) isFlag(x ast.Expr) bool {
	o := r.info.FeatureOf(x)
	return o != nil && o.Name == r.full
}

// mentions reports whether a node reads the removed flag.
func (r *remover) mentions(n ast.Node) bool {
	for _, o := range r.info.FeaturesIn(n) {
		if o.Name == r.full {
			return true
		}
	}
	return false
}

// simplify substitutes the flag value into a condition: known means the
// value is known (value), otherwise text is the simplified form.
func (r *remover) simplify(x ast.Expr) (known, value bool, text string) {
	switch e := x.(type) {
	case *ast.ParenExpr:
		k, v, t := r.simplify(e.X)
		if k {
			return k, v, ""
		}
		return false, false, "(" + t + ")"
	case *ast.UnaryExpr:
		if e.Op == token.NOT {
			k, v, t := r.simplify(e.X)
			if k {
				return true, !v, ""
			}
			return false, false, "not " + t
		}
	case *ast.BinaryExpr:
		if e.Op == token.AND || e.Op == token.OR {
			lk, lv, lt := r.simplify(e.X)
			rk, rv, rt := r.simplify(e.Y)
			and := e.Op == token.AND
			switch {
			case lk && lv != and: // false and …, true or …
				return true, lv, ""
			case rk && rv != and:
				return true, rv, ""
			case lk && rk:
				return true, lv, ""
			case lk:
				return false, false, rt
			case rk:
				return false, false, lt
			}
			return false, false, lt + " " + e.Op.String() + " " + rt
		}
	}
	if r.isFlag(x) {
		return true, r.on, ""
	}
	return false, false, r.text(x)
}

func boolText(v bool) string {
	if v {
		return "true"
	}
	return "false"
}

func (r *remover) file(f *ast.File) {
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FeatureDecl:
			if r.info.FeatureNames[d] == r.full {
				from := d.Pos()
				if d.Doc != nil {
					from = d.Doc.Pos()
				}
				r.edit(r.lineStart(from), r.lineEnd(d.End()), "")
			}
			continue
		case *ast.TestDecl:
			if r.test(d) {
				continue
			}
		}
		r.node(d)
	}
}

// test drops the flag from a test's feature line; with off it removes a
// test that turned the flag on (true means the test was removed).
func (r *remover) test(d *ast.TestDecl) bool {
	if d.Features == nil {
		return false
	}
	var keep []string
	enabled := false
	for _, id := range d.Features.Names {
		if o := r.info.Uses[id]; o != nil && o.Name == r.full {
			enabled = true
			continue
		}
		keep = append(keep, id.Name)
	}
	if !enabled {
		return false
	}
	if !r.on {
		from := d.Pos()
		if d.Doc != nil {
			from = d.Doc.Pos()
		}
		r.edit(r.lineStart(from), r.lineEnd(d.End()), "")
		r.tests = append(r.tests, d.Name.Value)
		return true
	}
	if len(keep) == 0 {
		r.edit(r.lineStart(d.Features.Pos()), r.lineEnd(d.Features.End()), "")
	} else {
		r.edit(r.off(d.Features.Pos()), r.off(d.Features.End()), "feature "+strings.Join(keep, ", "))
	}
	return false
}

// node walks a node: if conditions and layout lines with the flag are
// simplified, other reads of the flag are replaced with the value.
func (r *remover) node(n ast.Node) {
	ast.Inspect(n, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.TestFeatures:
			return false // test edits the feature line
		case *ast.Block:
			r.block(x)
			return false
		case *ast.IfStmt:
			if e, ok := x.Else.(*ast.IfStmt); ok && r.mentions(e.Cond) {
				r.node(x.Cond)
				r.node(x.Body)
				r.elseIf(x, e)
				return false
			}
		case *ast.PrimNode:
			return r.uiNode(x, x.IfPos, x.Cond, x.Children)
		case *ast.FieldNode:
			return r.uiNode(x, x.IfPos, x.Cond, x.Children)
		case ast.Expr:
			if r.isFlag(x) {
				r.edit(r.off(x.Pos()), r.off(x.End()), boolText(r.on))
				return false
			}
		}
		return true
	})
}

// block walks the statements of a block: an if with the flag in its
// condition is replaced with the branch that runs.
func (r *remover) block(b *ast.Block) {
	for _, s := range b.List {
		st, ok := s.(*ast.IfStmt)
		if !ok || !r.mentions(st.Cond) {
			r.node(s)
			continue
		}
		r.ifStmt(st, len(b.List) == 1)
	}
}

// ifStmt simplifies an if with the flag standing as a separate statement;
// only: it is the only one in the block (an empty block gets a return).
func (r *remover) ifStmt(s *ast.IfStmt, only bool) {
	known, value, cond := r.simplify(s.Cond)
	whole := [2]int{r.lineStart(s.If), r.lineEnd(s.End())}
	indent := strings.Repeat(" ", r.off(s.If)-whole[0])
	switch {
	case !known:
		r.edit(r.off(s.Cond.Pos()), r.off(s.Cond.End()), cond)
		r.node(s.Body)
		if s.Else != nil {
			r.node(s.Else)
		}
	case value:
		r.edit(whole[0], whole[1], r.dedent(s.Body, indent))
		r.node(s.Body)
	case s.Else == nil:
		text := ""
		if only {
			text = indent + "return\n"
		}
		r.edit(whole[0], whole[1], text)
	default:
		switch e := s.Else.(type) {
		case *ast.Block:
			r.edit(whole[0], whole[1], r.dedent(e, indent))
			r.node(e)
		case *ast.IfStmt:
			// else if x … becomes if x …
			r.edit(r.off(s.If), r.off(e.If), "")
			if r.mentions(e.Cond) {
				r.node(e.Body) // the flag in two conditions of one chain is nesting (E1502), it does not occur
			} else {
				r.node(e)
			}
		}
	}
}

// elseIf simplifies an else if with the flag in an if chain without the flag.
func (r *remover) elseIf(parent, e *ast.IfStmt) {
	known, value, cond := r.simplify(e.Cond)
	tail := [2]int{r.off(parent.ElsePos), r.lineEnd(e.End())}
	lines := func(b *ast.Block) string {
		if len(b.List) == 0 {
			return ""
		}
		return string(r.src[r.lineStart(b.List[0].Pos()):r.lineEnd(b.End())])
	}
	switch {
	case !known:
		r.edit(r.off(e.Cond.Pos()), r.off(e.Cond.End()), cond)
		r.node(e.Body)
		if e.Else != nil {
			r.node(e.Else)
		}
	case value:
		r.edit(tail[0], tail[1], "else\n"+lines(e.Body))
		r.node(e.Body)
	case e.Else == nil:
		r.edit(r.lineStart(parent.ElsePos), tail[1], "")
	default:
		switch ee := e.Else.(type) {
		case *ast.Block:
			r.edit(tail[0], tail[1], "else\n"+lines(ee))
			r.node(ee)
		case *ast.IfStmt:
			r.edit(r.off(e.If), r.off(ee.If), "")
			r.node(ee)
		}
	}
}

// dedent returns the lines of a block shifted to indentation indent.
func (r *remover) dedent(b *ast.Block, indent string) string {
	if len(b.List) == 0 {
		return ""
	}
	from, to := r.lineStart(b.List[0].Pos()), r.lineEnd(b.End())
	shift := r.off(b.List[0].Pos()) - from - len(indent)
	var out strings.Builder
	for _, line := range strings.SplitAfter(string(r.src[from:to]), "\n") {
		if line == "" {
			continue
		}
		n := 0
		for n < shift && n < len(line) && line[n] == ' ' {
			n++
		}
		out.WriteString(line[n:])
	}
	return out.String()
}

// uiNode simplifies the condition of a layout line: true removes the "if",
// false removes the line with its nested lines.
func (r *remover) uiNode(n ast.Node, ifPos source.Pos, cond ast.Expr, children []ast.UINode) bool {
	if cond == nil || !r.mentions(cond) {
		return true
	}
	known, value, text := r.simplify(cond)
	switch {
	case !known:
		r.edit(r.off(cond.Pos()), r.off(cond.End()), text)
	case value:
		from := r.off(ifPos)
		for from > 0 && r.src[from-1] == ' ' {
			from--
		}
		r.edit(from, r.off(cond.End()), "")
	default:
		r.edit(r.lineStart(n.Pos()), r.lineEnd(n.End()), "")
		return false
	}
	for _, c := range children {
		r.node(c)
	}
	return false
}
