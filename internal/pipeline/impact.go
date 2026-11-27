package pipeline

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/apicontract"
	"github.com/tycheg/reasonr-compiler/internal/impact"
	"github.com/tycheg/reasonr-compiler/internal/ir"
	"github.com/tycheg/reasonr-compiler/internal/lower"
	"github.com/tycheg/reasonr-compiler/internal/module"
	"github.com/tycheg/reasonr-compiler/internal/schemagen"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/uigen"
)

// Graph builds the test impact analysis graph of a model without errors
// (RFC §27.4). The "ui" node is the whole UI program (apps, screens,
// places, texts): its change affects every test on --driver web.
func Graph(m *Model) *impact.Graph {
	prog := lower.Program(m.Program, m.Info, m.Checks)
	g := impact.Build(prog, schemagen.Generate(m.Program, m.Info))
	if data, err := json.Marshal(uigen.Generate(m.Info)); err == nil {
		g.Hash[impact.UI] = impact.Hash(string(data))
	}
	return g
}

// IR returns the IR of a model without errors.
func IR(m *Model) *ir.Program { return lower.Program(m.Program, m.Info, m.Checks) }

// ImpactReport lists the affected tests of a module against the base version.
type ImpactReport struct {
	Version int    `json:"version"`
	Base    string `json:"base"`
	// Full: a full run is needed because something the analysis cannot see
	// changed; Reasons says why.
	Full    bool               `json:"full"`
	Reasons []string           `json:"reasons,omitempty"`
	Models  []*impact.Affected `json:"models"`
	// Violations: old tests changed together with introducing a flag.
	Violations []string `json:"violations,omitempty"`
}

// Selected returns the names of the tests of model ext to run; nil means
// all (a full run, the model is not in the report, or web — a run through
// the UI — while the UI changed).
func (r *ImpactReport) Selected(ext string, web bool) map[string]bool {
	if r.Full {
		return nil
	}
	for _, a := range r.Models {
		if a.Model == ext {
			if web && a.UI {
				return nil
			}
			out := map[string]bool{}
			for name := range a.Tests {
				out[name] = true
			}
			return out
		}
	}
	return nil
}

// Impact compares module dir with its version at git ref: which acceptance
// tests of each model (base and extensions) the change may affect. allowed
// lists the tests that may change when a flag is introduced.
func Impact(dir, ref string, allowed map[string]bool) (*ImpactReport, error) {
	rep := &ImpactReport{Version: 1, Base: ref}
	cur, err := Open(source.NewFileSet(), dir)
	if err != nil {
		return nil, err
	}
	baseDir, cleanup, reasons, err := checkoutBase(dir, ref)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	rep.Reasons = reasons
	deps, err := module.DepsOf(dir)
	if err != nil {
		return nil, err
	}
	fset := source.NewFileSet()
	bm, diags, err := module.LoadDeps(fset, os.DirFS(baseDir), baseDir, deps, nil)
	if err != nil {
		return nil, err
	}
	base := &Module{Module: bm, FileSet: fset, Diagnostics: diags}
	for _, ext := range cur.Select("") {
		cm, err := cur.Model(ext)
		if err != nil {
			return nil, err
		}
		if cm.HasErrors() {
			return nil, fmt.Errorf("model %s has errors: reasonr check", modelName(ext))
		}
		if ext != "" && base.Extension(ext) == nil {
			rep.Reasons = append(rep.Reasons, "new extension "+ext)
			continue
		}
		bmod, err := base.Model(ext)
		if err != nil || bmod.HasErrors() {
			rep.Reasons = append(rep.Reasons, fmt.Sprintf("model %s does not build in the base version", modelName(ext)))
			continue
		}
		cg, bg := Graph(cm), Graph(bmod)
		rep.Models = append(rep.Models, impact.Compare(ext, bg, cg))
		rep.Violations = append(rep.Violations, impact.FlagViolations(bg, cg, allowed)...)
	}
	rep.Full = len(rep.Reasons) > 0
	sort.Strings(rep.Violations)
	return rep, nil
}

func modelName(ext string) string {
	if ext == "" {
		return "base"
	}
	return ext
}

// checkoutBase extracts module dir at version ref from git into a temporary
// directory; reasons lists changes next to the sources that the analysis
// cannot see (reasonr.yaml, reasonr.lock, engine.yaml and any project files
// other than *.rr and api/): then a full run is needed.
func checkoutBase(dir, ref string) (string, func(), []string, error) {
	abs, err := filepath.Abs(dir)
	if err == nil {
		abs, err = filepath.EvalSymlinks(abs) // git names the root without links
	}
	if err != nil {
		return "", nil, nil, err
	}
	git := func(args ...string) (string, error) {
		cmd := exec.Command("git", append([]string{"-C", abs}, args...)...)
		var out, errOut bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errOut
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("git %s: %v %s", strings.Join(args, " "), err, strings.TrimSpace(errOut.String()))
		}
		return strings.TrimSpace(out.String()), nil
	}
	root, err := git("rev-parse", "--show-toplevel")
	if err != nil {
		return "", nil, nil, err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", nil, nil, err
	}
	// The base is the merge base with ref: changes that came to ref later do
	// not belong to the change under check.
	if mb, err := git("merge-base", "HEAD", ref); err == nil {
		ref = mb
	}
	tmp, err := os.MkdirTemp("", "reasonr-impact-")
	if err != nil {
		return "", nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	archive := exec.Command("git", "-C", root, "archive", "--format=tar", ref, "--", rel)
	untar := exec.Command("tar", "-x", "-C", tmp)
	pipe, _ := archive.StdoutPipe()
	untar.Stdin = pipe
	var errOut bytes.Buffer
	archive.Stderr = &errOut
	if err := untar.Start(); err != nil {
		cleanup()
		return "", nil, nil, err
	}
	if err := archive.Run(); err != nil {
		cleanup()
		return "", nil, nil, fmt.Errorf("git archive %s: %v %s", ref, err, strings.TrimSpace(errOut.String()))
	}
	if err := untar.Wait(); err != nil {
		cleanup()
		return "", nil, nil, err
	}
	// The project is the directory above the sources with reasonr.yaml, otherwise the sources themselves.
	project := filepath.Dir(abs)
	if _, err := os.Stat(filepath.Join(project, "reasonr.yaml")); err != nil {
		project = abs
	}
	prel, _ := filepath.Rel(root, project)
	changed, err := git("diff", "--name-only", ref, "--", filepath.Join(root, prel))
	if err != nil {
		cleanup()
		return "", nil, nil, err
	}
	untracked, err := git("ls-files", "--others", "--exclude-standard", "--full-name", "--", filepath.Join(root, prel))
	if err != nil {
		cleanup()
		return "", nil, nil, err
	}
	var reasons []string
	for _, f := range strings.Fields(changed + "\n" + untracked) {
		inSrc := strings.HasPrefix(f, rel+"/")
		switch {
		case inSrc && strings.HasSuffix(f, ".rr"), inSrc && strings.HasPrefix(f, filepath.Join(rel, apicontract.Dir)+"/"),
			strings.HasSuffix(f, ".md"):
		default:
			reasons = append(reasons, "changed "+f)
		}
	}
	return filepath.Join(tmp, rel), cleanup, reasons, nil
}
