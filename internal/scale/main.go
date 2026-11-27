// Command scale measures the compiler at scale (docs/plans/0015): time and
// memory by phase on a genmodel model, pprof profiles, comparison with a
// previous report and budgets.
//
//	go run ./internal/scale --lines 1m [--model dir] [--profile dir]
//	  [--baseline previous.json] [-o report.json] [--budget]
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"strings"
	"syscall"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/check"
	"github.com/tycheg/reasonr-compiler/internal/codegen"
	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/genmodel"
	"github.com/tycheg/reasonr-compiler/internal/ir"
	"github.com/tycheg/reasonr-compiler/internal/lower"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	"github.com/tycheg/reasonr-compiler/internal/schemagen"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/uigen"
)

// Report is a measurement report.
type Report struct {
	Version int              `json:"version"`
	Model   genmodel.Stats   `json:"model"`
	CPUs    int              `json:"cpus"`
	Phases  []Phase          `json:"phases"`
	Totals  map[string]int64 `json:"totals"`   // check, build, incremental — ms
	MaxRSS  int64            `json:"maxRssMB"` // process peak: check, build and the repeated check together
	// CheckHeap is the live heap after check, MB.
	CheckHeap int64    `json:"checkHeapMB"`
	Failed    []string `json:"failed,omitempty"` // broken budgets and regressions
}

// Phase is a phase: time and allocated memory.
type Phase struct {
	Name    string `json:"name"`
	Ms      int64  `json:"ms"`
	AllocMB int64  `json:"allocMB"`
	HeapMB  int64  `json:"heapMB"` // live heap after the phase
}

// Budgets for 10 million lines (plan 0015, decision 2), scaled linearly to
// the model size: ms and MB per million lines.
var budgets = map[string]float64{"check": 6_000, "build": 9_000, "incremental": 300}

// heapBudgetPerMillion is the live heap after check: the 16 GB budget for
// 10 million lines is the peak process memory, which is about twice the
// live heap (GOGC=100).
const heapBudgetPerMillion = 800 // MB

func main() {
	lines := flag.String("lines", "1m", "size of the synthetic model: 100k, 1m, 10m")
	seed := flag.Uint64("seed", 1, "generator seed")
	dir := flag.String("model", "", "model directory: already generated or where to generate (a temporary one by default)")
	profile := flag.String("profile", "", "pprof profile directory: CPU and heap by phase")
	baseline := flag.String("baseline", "", "previous report: a phase regression over 20 % is a failure")
	out := flag.String("o", "", "JSON report file (stdout by default)")
	budget := flag.Bool("budget", false, "check the plan 0015 budgets (scaled to the model size)")
	flag.Parse()
	if err := run(*lines, *seed, *dir, *profile, *baseline, *out, *budget); err != nil {
		fmt.Fprintln(os.Stderr, "scale:", err)
		os.Exit(2)
	}
}

func run(lines string, seed uint64, dir, profile, baseline, out string, budget bool) error {
	n, err := genmodel.ParseLines(lines)
	if err != nil {
		return err
	}
	rep := &Report{Version: 1, CPUs: runtime.NumCPU(), Totals: map[string]int64{}}
	if dir == "" {
		tmp, err := os.MkdirTemp("", "reasonr-scale-")
		if err != nil {
			return err
		}
		defer os.RemoveAll(tmp)
		dir = tmp
	}
	if entries, _ := os.ReadDir(dir); len(entries) == 0 {
		fmt.Fprintf(os.Stderr, "generating %s lines in %s…\n", lines, dir)
		if rep.Model, err = genmodel.Generate(dir, genmodel.Options{Lines: n, Seed: seed}); err != nil {
			return err
		}
	} else if rep.Model, err = stats(dir); err != nil {
		return err
	}
	if profile != "" {
		if err := os.MkdirAll(profile, 0o755); err != nil {
			return err
		}
	}
	p := &phases{rep: rep, profile: profile}

	// reasonr check: loading and parsing, program assembly, names and types, checks.
	fset := source.NewFileSet()
	cache := module.NewCache()
	var mod *pipeline.Module
	p.run("load", func() { mod, err = pipeline.OpenCached(fset, os.DirFS(dir), dir, cache) })
	if err != nil {
		return err
	}
	var prog *module.Program
	p.run("program", func() { prog, _, err = mod.Program("") })
	if err != nil {
		return err
	}
	var info *sema.Info
	var diags []*diag.Diagnostic
	p.run("sema", func() { info, diags = sema.Check(prog) })
	var res *check.Result
	p.run("check", func() { res = check.Analyze(prog, info, check.All) })
	diags = append(append(diags, mod.Diagnostics...), res.Diagnostics...)
	if diag.HasErrors(diags) {
		return fmt.Errorf("the model has errors: %d diagnostics, the first: %s", len(diags), diags[0].Message())
	}
	rep.Totals["check"] = p.sum("load", "program", "sema", "check")

	// reasonr build: IR, schema, bytecode, UI program.
	var irp *ir.Program
	p.run("lower", func() { irp = lower.Program(prog, info, res) })
	p.run("verify", func() { err = ir.Verify(irp) })
	if err != nil {
		return err
	}
	sc := schemagen.Generate(prog, info)
	p.run("schema", func() { sc = schemagen.Generate(prog, info) })
	p.run("codegen", func() { _, err = codegen.Generate(irp, sc) })
	if err != nil {
		return err
	}
	p.run("ui", func() { uigen.Generate(info) })
	rep.Totals["build"] = rep.Totals["check"] + p.sum("lower", "verify", "schema", "codegen", "ui")

	// A repeated check after editing one file: parsing comes from the cache.
	edited := filepath.Join(dir, "contexts", "C0000", "item0.rr")
	orig, err := os.ReadFile(edited)
	if err != nil {
		return err
	}
	defer os.WriteFile(edited, orig, 0o644)
	if err := os.WriteFile(edited, append(append([]byte(nil), orig...), "\n# edit\n"...), 0o644); err != nil {
		return err
	}
	p.run("incremental", func() {
		var m *pipeline.Module
		if m, err = pipeline.OpenCached(fset, os.DirFS(dir), dir, cache); err == nil {
			_, err = m.Model("")
		}
	})
	if err != nil {
		return err
	}
	rep.Totals["incremental"] = p.sum("incremental")

	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) == nil {
		rep.MaxRSS = maxRSS(ru) >> 20
	}
	for _, ph := range rep.Phases {
		if ph.Name == "check" {
			rep.CheckHeap = ph.HeapMB
		}
	}
	if budget {
		million := float64(rep.Model.Lines) / 1e6
		for name, per := range budgets {
			if limit := int64(per * million); rep.Totals[name] > limit {
				rep.Failed = append(rep.Failed, fmt.Sprintf("%s: %d ms, budget %d ms", name, rep.Totals[name], limit))
			}
		}
		if limit := int64(heapBudgetPerMillion * million); rep.CheckHeap > limit {
			rep.Failed = append(rep.Failed, fmt.Sprintf("check memory: %d MB of live heap, budget %d MB", rep.CheckHeap, limit))
		}
	}
	if baseline != "" {
		if err := compare(rep, baseline); err != nil {
			return err
		}
	}
	data, _ := json.MarshalIndent(rep, "", "  ")
	data = append(data, '\n')
	if out != "" {
		err = os.WriteFile(out, data, 0o644)
	} else {
		_, err = os.Stdout.Write(data)
	}
	if err != nil {
		return err
	}
	for _, f := range rep.Failed {
		fmt.Fprintln(os.Stderr, "failure:", f)
	}
	if len(rep.Failed) > 0 {
		os.Exit(1)
	}
	return nil
}

type phases struct {
	rep     *Report
	profile string
}

// run measures a phase: time, allocated memory and the live heap after it;
// with profiles, the phase CPU profile and the heap profile after it.
func (p *phases) run(name string, f func()) {
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	var cpu *os.File
	if p.profile != "" {
		cpu, _ = os.Create(filepath.Join(p.profile, name+".cpu.pprof"))
		_ = pprof.StartCPUProfile(cpu)
	}
	start := time.Now()
	f()
	ms := time.Since(start).Milliseconds()
	if cpu != nil {
		pprof.StopCPUProfile()
		cpu.Close()
		if heap, err := os.Create(filepath.Join(p.profile, name+".heap.pprof")); err == nil {
			_ = pprof.WriteHeapProfile(heap)
			heap.Close()
		}
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	p.rep.Phases = append(p.rep.Phases, Phase{Name: name, Ms: ms,
		AllocMB: int64(after.TotalAlloc-before.TotalAlloc) >> 20, HeapMB: int64(after.HeapAlloc) >> 20})
	fmt.Fprintf(os.Stderr, "%-12s %8d ms %8d MB allocated %6d MB heap\n", name, ms, (after.TotalAlloc-before.TotalAlloc)>>20, after.HeapAlloc>>20)
}

func (p *phases) sum(names ...string) int64 {
	var t int64
	for _, ph := range p.rep.Phases {
		for _, n := range names {
			if ph.Name == n {
				t += ph.Ms
			}
		}
	}
	return t
}

// compare compares phases with a previous report of the same model: 20 %
// slower (and by at least 100 ms, to cut noise) is a regression.
func compare(rep *Report, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var old Report
	if err := json.Unmarshal(data, &old); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if old.Model.Lines != rep.Model.Lines {
		return fmt.Errorf("%s: a different model (%d lines, now %d)", path, old.Model.Lines, rep.Model.Lines)
	}
	for _, ph := range rep.Phases {
		for _, o := range old.Phases {
			if o.Name == ph.Name && ph.Ms > o.Ms*12/10 && ph.Ms-o.Ms >= 100 {
				rep.Failed = append(rep.Failed, fmt.Sprintf("regression %s: %d ms, was %d ms", ph.Name, ph.Ms, o.Ms))
			}
		}
	}
	return nil
}

// stats measures the size of an already generated model.
func stats(dir string) (genmodel.Stats, error) {
	var st genmodel.Stats
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".rr") {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		st.Files++
		st.Lines += strings.Count(string(data), "\n")
		return nil
	})
	return st, err
}

// maxRSS is the peak resident memory of the process in bytes: Maxrss is in
// kilobytes on Linux and in bytes on macOS.
func maxRSS(ru syscall.Rusage) int64 {
	if runtime.GOOS == "darwin" {
		return int64(ru.Maxrss)
	}
	return int64(ru.Maxrss) << 10
}
