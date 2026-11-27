// Package check holds model checks after type checking
// (RFC §28). Each check is a separate analyzer
// with a common interface, so analyzers can be enabled and tested one by one.
package check

import (
	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/types"
)

// Analyzer is one model check.
type Analyzer struct {
	Name     string
	Doc      string
	Codes    []diag.Code
	Requires []*Analyzer
	// Run performs the check and returns a result for dependent analyzers.
	Run func(*Pass) any
}

// Pass is the data for an analyzer.
type Pass struct {
	Program *module.Program
	Info    *sema.Info

	bag     *diag.Bag
	results map[*Analyzer]any
}

// Report reports a diagnostic.
func (p *Pass) Report(spec *diag.Spec, r source.Range, args ...any) *diag.Diagnostic {
	return p.bag.Add(spec, r, args...)
}

// ResultOf returns the result of an analyzer from Requires.
func (p *Pass) ResultOf(a *Analyzer) any { return p.results[a] }

// File returns the source file a position belongs to.
func (p *Pass) File(pos source.Pos) *source.File {
	if f := p.Program.FileAt(pos); f != nil {
		return f.Src
	}
	return nil
}

// All lists all analyzers in execution order.
var All = []*Analyzer{CallGraph, Queries, Unique, Access, PII, Lint}

// Result holds the analyzer results and their diagnostics.
type Result struct {
	Diagnostics []*diag.Diagnostic
	results     map[*Analyzer]any
}

// Of returns the result of an analyzer, or nil if it did not run.
func (r *Result) Of(a *Analyzer) any { return r.results[a] }

// Searches returns the parsed searches (the result of Queries).
func (r *Result) Searches() map[*ast.SearchExpr]*Search {
	m, _ := r.results[Queries].(map[*ast.SearchExpr]*Search)
	return m
}

// Run runs the analyzers (with dependencies) and returns the diagnostics.
func Run(p *module.Program, info *sema.Info, analyzers []*Analyzer) []*diag.Diagnostic {
	return Analyze(p, info, analyzers).Diagnostics
}

// Analyze runs the analyzers (with dependencies) and returns their results.
func Analyze(p *module.Program, info *sema.Info, analyzers []*Analyzer) *Result {
	var bag diag.Bag
	pass := &Pass{Program: p, Info: info, bag: &bag, results: map[*Analyzer]any{}}
	done := map[*Analyzer]bool{}
	var run func(a *Analyzer)
	run = func(a *Analyzer) {
		if done[a] {
			return
		}
		done[a] = true
		for _, r := range a.Requires {
			run(r)
		}
		pass.results[a] = a.Run(pass)
	}
	for _, a := range analyzers {
		run(a)
	}
	list := bag.List()
	diag.Sort(list)
	return &Result{Diagnostics: list, results: pass.results}
}

func rangeOf(n ast.Node) source.Range { return source.Range{Start: n.Pos(), End: n.End()} }

// member is a type member with a body and its owner.
type member struct {
	decl *types.Decl
	node ast.Member
	body *ast.Block
}

// bodies returns the members with bodies: commands, events, queries, rules,
// value methods, on reactions.
func bodies(info *sema.Info) []member {
	var out []member
	for _, d := range info.DeclList {
		var members []ast.Member
		switch n := d.Node.(type) {
		case *ast.EntityDecl:
			members = n.Members
		case *ast.ProjectionDecl:
			members = n.Members
		case *ast.ValueDecl:
			members = n.Members
		}
		for _, m := range members {
			var b *ast.Block
			switch m := m.(type) {
			case *ast.CommandDecl:
				b = m.Body
			case *ast.EventDecl:
				b = m.Body
			case *ast.QueryDecl:
				b = m.Body
			case *ast.RuleDecl:
				b = m.Body
			case *ast.MethodDecl:
				b = m.Body
			case *ast.OnDecl:
				b = m.Body
			}
			if b != nil {
				out = append(out, member{decl: d, node: m, body: b})
			}
		}
	}
	return out
}

// calls returns the resolved calls in a node without entering schedule
// arguments: a timer runs in a separate unit of work.
func calls(info *sema.Info, n ast.Node) []*ast.CallExpr {
	var out []*ast.CallExpr
	ast.Inspect(n, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if c := info.Calls[call]; c != nil {
			if c.Kind == sema.CallBuiltin && c.Builtin == "schedule" {
				return false
			}
			out = append(out, call)
		}
		return true
	})
	return out
}
