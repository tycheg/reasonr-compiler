package sema

import (
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

// Feature flags (RFC §23.4): a flag belongs to a
// system, like a role, and is read as a boolean where call code runs
// (commands, queries, rules, on reactions) and in layouts. The engine
// computes the value per unit of work; events must not read it, since
// replay must not depend on it. Flags do not nest: a condition with a flag
// inside a branch of a condition with a flag, and two flags in one
// condition, are errors.

// MaxFeatures is how many flags a model may have: the caller's flag set is
// a fixed-size bit set (model.Features).
const MaxFeatures = model.MaxFeatures

var (
	featureDate = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

	whyFeatureReplay = "events must be replayable"
	whyFeaturePure   = "a flag value belongs to a call, and this is evaluated without a caller"
	whyFeatureTest   = "a test enables flags with a feature line at its start"
	whyFeatureFake   = "a fake sees only request arguments and its own fields"
)

// collectFeature collects a flag declaration: the name, uniqueness in the system, the date.
func (c *checker) collectFeature(f *ast.FeatureDecl) {
	c.lowerName(f.Name, kindFeature)
	if builtinNames[f.Name.Name] {
		c.bag.Add(diag.E0108, rangeOf(f.Name), f.Name.Name)
	}
	if f.Date != nil && f.Date.Raw != "" {
		if _, err := time.Parse(time.DateOnly, f.Date.Value); err != nil || !featureDate.MatchString(f.Date.Value) {
			c.bag.Add(diag.E1501, rangeOf(f.Date), f.Name.Name, f.Date.Raw)
		}
	}
	full := qualify(c.ctxOf(f), f.Name.Name)
	if prev, dup := c.info.Features[full]; dup {
		c.bag.Add(diag.E0101, rangeOf(f.Name), f.Name.Name, "flags of a context must be unique").
			Note(rangeOf(prev.Name), noteDeclared)
		return
	}
	c.info.Features[full] = f
	c.info.FeatureNames[f] = full
	if n := len(c.info.Features); n == MaxFeatures+1 {
		c.bag.Add(diag.E1506, rangeOf(f.Name), n, MaxFeatures)
	}
}

// builtinNames are the built-in values: a flag cannot have such a name.
var builtinNames = map[string]bool{
	"id": true, "now": true, "today": true, "after": true, "hasErrors": true, "me": true, "caller": true, "self": true,
}

// feature returns a flag by name from place at: of its own bounded context,
// "Context.flag" or, outside bounded contexts and in tests, the only flag
// with this name in the project.
func (c *checker) feature(at ast.Node, name string) *ast.FeatureDecl {
	if len(c.info.Features) == 0 {
		return nil
	}
	if strings.Contains(name, ".") {
		return c.info.Features[name]
	}
	ctx := c.ctxOf(at)
	if f := c.info.Features[qualify(ctx, name)]; f != nil {
		return f
	}
	if ctx != "" && !c.inTest(at) {
		return nil
	}
	var found *ast.FeatureDecl
	for _, f := range c.info.Features {
		if f.Name.Name == name {
			if found != nil {
				return nil
			}
			found = f
		}
	}
	return found
}

// featureRead checks the read of flag f by name id: where it is read and
// whether it is nested in a branch of another flag.
func (b *body) featureRead(id *ast.Ident, f *ast.FeatureDecl) *Object {
	full := b.c.info.FeatureNames[f]
	o := &Object{Kind: ObjFeature, Name: full, Type: tBool, Node: f, Feature: f}
	var why string
	switch {
	case b.fake():
		why = whyFeatureFake
	case b.kind == ctxEvent:
		why = whyFeatureReplay
	case b.kind == ctxTest:
		why = whyFeatureTest
	case b.kind == ctxCommand, b.kind == ctxQuery, b.kind == ctxRule, b.kind == ctxOn, b.kind == ctxUI:
	default:
		why = whyFeaturePure
	}
	if why != "" {
		b.c.bag.Add(diag.E1504, rangeOf(id), ctxNames[b.kind], full, why)
		o.Type = invalid
		return o
	}
	if b.flag != nil {
		b.c.bag.Add(diag.E1502, rangeOf(id), full, b.c.info.Uses[b.flag].Name)
	}
	b.flagReads = append(b.flagReads, id)
	return o
}

// featureSelector resolves a flag by full name: Ordering.tips. nil means
// the expression names no flag. A flag of another bounded context is read
// only outside bounded contexts (apps) and in tests.
func (b *body) featureSelector(x *ast.SelectorExpr) *Object {
	cx, ok := x.X.(*ast.Ident)
	if !ok || x.Safe || !b.c.contexts[cx.Name] || b.lookupQuiet(cx.Name) != nil || b.fieldNamed(cx.Name) {
		return nil
	}
	f := b.c.info.Features[cx.Name+"."+x.Sel.Name]
	if f == nil {
		return nil
	}
	if ctx := b.c.ctxOf(x); ctx != "" && ctx != cx.Name && !b.c.inTest(x) {
		b.c.bag.Add(diag.E1407, rangeOf(x), cx.Name+"."+x.Sel.Name, cx.Name)
		return &Object{Kind: ObjFeature, Name: cx.Name + "." + x.Sel.Name, Type: invalid, Node: f, Feature: f}
	}
	o := b.featureRead(x.Sel, f)
	b.c.info.Uses[x.Sel] = o
	return o
}

// condFlag checks the flags of condition cond, already checked by check: a
// second flag is E1503. It returns the condition's flag or nil.
func (b *body) condFlag(reads []*ast.Ident) *ast.Ident {
	if len(reads) == 0 {
		return nil
	}
	for _, id := range reads[1:] {
		b.c.bag.Add(diag.E1503, rangeOf(id), b.c.info.Uses[reads[0]].Name)
	}
	return reads[0]
}

// checkTestFeatures resolves the flags of a test's feature line.
func (c *checker) checkTestFeatures(d *ast.TestDecl) {
	if d.Features == nil {
		return
	}
	seen := map[string]bool{}
	var names []string
	for _, id := range d.Features.Names {
		f := c.feature(id, id.Name)
		if f == nil {
			c.bag.Add(diag.E1505, rangeOf(id), id.Name, id.Name)
			continue
		}
		full := c.info.FeatureNames[f]
		c.info.Uses[id] = &Object{Kind: ObjFeature, Name: full, Type: tBool, Node: f, Feature: f}
		if !seen[full] {
			seen[full] = true
			names = append(names, full)
		}
	}
	sort.Strings(names)
	c.info.TestFeatures[d] = names
}

// checkFeatureTests warns about project flags no test turns on (W1501). The
// production program has no tests; the tests of dependency modules check
// their flags.
func (c *checker) checkFeatureTests() {
	if c.prog.Production {
		return
	}
	enabled := map[string]bool{}
	for _, names := range c.info.TestFeatures {
		for _, n := range names {
			enabled[n] = true
		}
	}
	for _, full := range FeatureList(c.info) {
		f := c.info.Features[full]
		if enabled[full] || c.depFile(f.Pos()) {
			continue
		}
		c.bag.Add(diag.W1501, rangeOf(f.Name), full, f.Name.Name)
	}
}

// depFile reports whether a position lies in a file of a dependency module.
func (c *checker) depFile(pos source.Pos) bool {
	if f := c.prog.FileAt(pos); f != nil {
		return f.Dep != ""
	}
	return false
}

// FeatureList returns the full names of the model's flags in ascending
// order: the number in the list is the flag's bit in the caller's set
// (model.Features).
func FeatureList(info *Info) []string {
	out := make([]string, 0, len(info.Features))
	for name := range info.Features {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// isFeature reports whether a name refers to a flag.
func (i *Info) isFeature(id *ast.Ident) bool {
	o := i.Uses[id]
	return o != nil && o.Kind == ObjFeature
}

// FeatureOf returns the flag expression x reads (a name or System.flag), or nil.
func (i *Info) FeatureOf(x ast.Expr) *Object {
	switch x := x.(type) {
	case *ast.Ident:
		if i.isFeature(x) {
			return i.Uses[x]
		}
	case *ast.SelectorExpr:
		if i.isFeature(x.Sel) {
			return i.Uses[x.Sel]
		}
	}
	return nil
}

// FeaturesIn returns the flags node x reads, in order.
func (i *Info) FeaturesIn(x ast.Node) []*Object {
	var out []*Object
	ast.Inspect(x, func(n ast.Node) bool {
		if e, ok := n.(ast.Expr); ok {
			if o := i.FeatureOf(e); o != nil {
				out = append(out, o)
				return false
			}
		}
		return true
	})
	return out
}
