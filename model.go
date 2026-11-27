package reasonr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/ast"
	"github.com/tycheg/reasonr-compiler/internal/compat"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/ir"
	"github.com/tycheg/reasonr-compiler/internal/lower"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
	"github.com/tycheg/reasonr-compiler/internal/runtime/vm"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/uigen"
)

// Model is a built model: the program the interpreter runs and what the
// engine asks about it. It is safe for concurrent calls.
type Model struct {
	// pm is the checked model when it is built from the sources; nil when
	// it is read from a model file (Open).
	pm   *pipeline.Model
	bc   *bytecode.Program
	mach *vm.Machine
	ext  string
	test bool

	scenarios []Scenario
	perf      map[string][]string // the source texts of performance checks by test
	files     map[string]string
	hash      string

	uiOnce sync.Once
	ui     *UI

	hashOnce sync.Once
	progHash string
}

var _ Interpreter = (*Model)(nil)

func newModel(pm *pipeline.Model) (*Model, error) {
	bc, err := pipeline.Build(pm)
	if err != nil {
		return nil, fmt.Errorf("compiler fault: %w", err)
	}
	mach, err := vm.New(bc, vm.DefaultLimits)
	if err != nil {
		return nil, fmt.Errorf("compiler fault: %w", err)
	}
	m := &Model{pm: pm, bc: bc, mach: mach, ext: pm.Program.Extension, test: !pm.Program.Production,
		files: map[string]string{}, perf: map[string][]string{}}
	m.index()
	return m, nil
}

// index fills what the engine asks about the sources: the scenarios, the
// performance checks, the files and their hash.
func (m *Model) index() {
	p, fset := m.pm.Program, m.pm.Module.FileSet
	for _, d := range p.Decls {
		t, ok := d.(*ast.TestDecl)
		if !ok {
			continue
		}
		pos, e := fset.Position(t.Pos()), fset.Position(t.End())
		end := e.Line
		if e.Column == 1 && end > pos.Line {
			end-- // the end is at the start of the next line
		}
		s := Scenario{Name: t.Name.Value, File: pos.Filename, Line: pos.Line, End: end}
		if f := p.FileAt(t.Pos()); f != nil {
			switch {
			case f.Demo:
				s.Kind = ScenarioDemo
			case f.Perf:
				s.Kind = ScenarioPerf
				if t.Then != nil {
					checks := make([]string, len(t.Then.Items))
					for i, it := range t.Then.Items {
						checks[i] = m.pm.Source(source.Range{Start: it.Pos(), End: it.End()})
					}
					m.perf[s.Name] = checks
				}
			}
		}
		m.scenarios = append(m.scenarios, s)
	}
	files := slices.Clone(p.Files)
	sort.Slice(files, func(i, j int) bool { return files[i].Rel < files[j].Rel })
	h := sha256.New()
	for _, f := range files {
		m.files[f.Rel] = f.Name
		fmt.Fprintf(h, "%s\x00%d\x00", f.Rel, f.Src.Size())
		h.Write(f.Src.Source())
	}
	m.hash = hex.EncodeToString(h.Sum(nil)[:12])
}

// Extension is the model's extension; "" is the base model.
func (m *Model) Extension() string { return m.ext }

// Test reports whether it is the test program: with tests, scenarios and
// fakes.
func (m *Model) Test() bool { return m.test }

// Schema is the storage and API schema of the model (RFC §30.5).
func (m *Model) Schema() *Schema { return m.bc.Schema }

// Execute runs a command and returns its unit of work (RFC §30.2).
func (m *Model) Execute(ctx context.Context, call CommandCall, env Env) (*Outcome, error) {
	return m.mach.Execute(ctx, call, env)
}

// Query runs a query.
func (m *Model) Query(ctx context.Context, call QueryCall, env Env) (*QueryOutcome, error) {
	return m.mach.Query(ctx, call, env)
}

// Migrate runs the manual migration name (RFC §32): each unit of work goes
// to commit; now is the migration time.
func (m *Model) Migrate(ctx context.Context, name string, env Env, now time.Time, commit func(*Outcome) error) error {
	return m.mach.Migrate(ctx, name, env, now, commit)
}

// Fold rebuilds the state of entity t with identifier id from its events
// (in the arena src) into the arena a.
func (m *Model) Fold(ctx context.Context, a, src *Arena, t TypeRef, id Value, events []Event) (Record, error) {
	return m.mach.Fold(ctx, a, src, t, id, events)
}

// Eval evaluates the interface function name (a screen condition, a
// computed argument) with arguments args in the arena a.
func (m *Model) Eval(ctx context.Context, name string, args []Value, a *Arena, caller Caller, now time.Time) (Value, error) {
	return m.mach.Eval(ctx, name, args, a, caller, now)
}

// UI is the interface program of the model (RFC §22): apps, screens,
// primitives, texts.
func (m *Model) UI() *UI {
	m.uiOnce.Do(func() {
		if m.ui == nil {
			m.ui = uigen.Generate(m.pm.Info)
		}
	})
	return m.ui
}

// ScenarioKind is the kind of a scenario.
type ScenarioKind uint8

const (
	ScenarioTest ScenarioKind = iota // an acceptance test (*.test.rr)
	ScenarioDemo                     // a demo scenario (*.demo.rr, RFC §27.5)
	ScenarioPerf                     // a performance test (*.perf.rr, RFC §27.6)
)

// Scenario is a test, a demo or a performance scenario of the model.
type Scenario struct {
	Name string
	Kind ScenarioKind
	// File is the path of its file as the module was loaded; Line and End
	// are its first and last lines.
	File      string
	Line, End int
}

// Scenarios are the model's scenarios in declaration order. A production
// model has none.
func (m *Model) Scenarios() []Scenario { return slices.Clone(m.scenarios) }

// Tests are the names of the program's runnable tests (acceptance, demo and
// performance) in declaration order.
func (m *Model) Tests() []string { return m.mach.Tests() }

// RunScenario runs the test name on the driver d. error means there is no
// such test (ErrNotFound); failures and runtime faults are in the result.
func (m *Model) RunScenario(ctx context.Context, name string, d Driver, o TestOptions) (*TestResult, error) {
	return m.mach.RunTestWith(ctx, name, d, o)
}

// Files maps the paths of the model's files relative to the module (as
// test failures name them) to their paths as the module was loaded.
func (m *Model) Files() map[string]string { return maps.Clone(m.files) }

// SourceHash is the hash of the model sources: the file paths and texts.
func (m *Model) SourceHash() string { return m.hash }

// PerfChecks are the source texts of the then checks of the performance
// test name, in order.
func (m *Model) PerfChecks(name string) []string { return slices.Clone(m.perf[name]) }

// PerfCheck evaluates the check i of the performance test name: stat
// returns a statistics field by its key ("cur|method|field" or
// "base|method|field"). A check that reads the baseline exists only in its
// baseline form: without baseline it is ErrNotFound.
func (m *Model) PerfCheck(ctx context.Context, name string, i int, baseline bool, stat func(key string) (Value, error)) (bool, error) {
	return m.mach.PerfCheck(ctx, lower.PerfCheck(name, i, baseline), stat)
}

// Compatible checks that the model reads the data stored with the schema
// prev (RFC §32): nil, or the errors (E11xx) that need a manual migration.
//
// A model read from a model file has no sources: its errors have no places.
func (m *Model) Compatible(prev *Schema) *Errors {
	if m.pm == nil {
		diags := compat.CheckSchema(prev, m.bc.Schema)
		if !diag.HasErrors(diags) {
			return nil
		}
		return render(source.NewFileSet(), diags)
	}
	diags := compat.Check(prev, m.pm.Program, m.pm.Info)
	if !diag.HasErrors(diags) {
		return nil
	}
	return render(m.pm.Module.FileSet, diags)
}

// CommandReads are the projections read by the functions that run in
// commands (commands, event application, rules, methods, reactions):
// projection name → the first such function. Queries read projections
// freely.
func (m *Model) CommandReads() map[string]string {
	p := m.bc
	out := map[string]string{}
	for i := range p.Funcs {
		f := &p.Funcs[i]
		switch f.Kind {
		case bytecode.FuncCommand, bytecode.FuncApply, bytecode.FuncRule, bytecode.FuncMethod, bytecode.FuncReaction:
		default:
			continue
		}
		for pc := 0; pc < len(f.Code); {
			in, ok := bytecode.DecodeInstr(f.Code, pc)
			if !ok {
				break
			}
			pc += in.Size
			if in.Op != bytecode.OpRecLoad && in.Op != bytecode.OpRefExists {
				continue
			}
			if ti := p.Schema.Type(TypeRef(in.C)); ti != nil && ti.Kind == TypeProjection {
				if _, seen := out[ti.Name]; !seen {
					out[ti.Name] = f.Name
				}
			}
		}
	}
	return out
}

// PrimitiveUse is a primitive an app of a platform draws.
type PrimitiveUse struct {
	Name string
	// Std: the standard catalog declares it (std/ui.rr).
	Std bool
}

// Primitives are the primitives the apps of platform draw (their nodes and
// screens), in name order.
func (m *Model) Primitives(platform string) []PrimitiveUse {
	ui := m.UI()
	std := map[string]bool{}
	for _, p := range ui.Primitives {
		if p.Std {
			std[p.Name] = true
		}
	}
	seen := map[string]bool{}
	var walk func([]UINode)
	walk = func(nodes []UINode) {
		for _, n := range nodes {
			if n.Primitive != "" {
				seen[n.Primitive] = true
			}
			walk(n.Children)
		}
	}
	for _, app := range ui.Apps {
		if !slices.Contains(app.Platforms, platform) {
			continue
		}
		walk(app.Nodes)
		for _, s := range app.Screens {
			if sc := ui.Screen(s); sc != nil {
				walk(sc.Nodes)
			}
		}
	}
	out := make([]PrimitiveUse, 0, len(seen))
	for n := range seen {
		out = append(out, PrimitiveUse{Name: n, Std: std[n]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// ImpactReport is the test impact analysis of a change (RFC §27.4).
type ImpactReport = pipeline.ImpactReport

// Impact analyzes the change of the module in dir against the git
// revision ref: which tests of each model it affects.
func Impact(dir, ref string) (*ImpactReport, error) { return pipeline.Impact(dir, ref, nil) }

// ImpactNodes are the nodes of the test impact analysis graph each test
// reaches (RFC §27.4): test name → nodes ("func command Ordering.Order.place").
func (m *Model) ImpactNodes() (map[string][]string, error) {
	if m.pm == nil {
		return nil, errors.New("a model from a model file has no sources")
	}
	return pipeline.Graph(m.pm).Tests, nil
}

// Mutate calls fn with each mutant of the model: the model with one
// function broken (its entry loops until the iteration limit) and the
// analysis node of that function. It checks the test impact analysis: a
// test a mutant fails must reach the broken node.
func (m *Model) Mutate(fn func(node string, mutant *Model) error) error {
	if m.pm == nil {
		return errors.New("a model from a model file has no sources")
	}
	base := pipeline.IR(m.pm)
	for i, f := range base.Funcs {
		switch f.Kind {
		case ir.FuncTest, ir.FuncMigration, ir.FuncUI:
			continue
		}
		prog := pipeline.IR(m.pm)
		broken := prog.Funcs[i]
		broken.Blocks = append([]*ir.Block{{Instrs: []ir.Instr{
			{Op: ir.OpLoopGuard, Dst: ir.NoSlot},
			{Op: ir.OpJmp, Dst: ir.NoSlot, Targets: []int{0}},
		}}}, broken.Blocks[1:]...)
		bc, err := pipeline.BuildIR(m.pm, prog)
		if err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
		mach, err := vm.New(bc, vm.DefaultLimits)
		if err != nil {
			return fmt.Errorf("%s: %w", f.Name, err)
		}
		mut := &Model{pm: m.pm, bc: bc, mach: mach, ext: m.ext, test: m.test,
			scenarios: m.scenarios, perf: m.perf, files: m.files, hash: m.hash}
		if err := fn("func "+f.Kind.String()+" "+f.Name, mut); err != nil {
			return err
		}
	}
	return nil
}

// ProgramHash is the hash of the program the interpreter runs: two
// processes run the same model when their hashes are equal.
func (m *Model) ProgramHash() string {
	m.hashOnce.Do(func() {
		h := sha256.New()
		if b, err := bytecode.Encode(m.bc); err == nil {
			h.Write(b)
		}
		m.progHash = hex.EncodeToString(h.Sum(nil)[:16])
	})
	return m.progHash
}
