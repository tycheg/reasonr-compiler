package uigen_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/format"
	"github.com/tycheg/reasonr-compiler/internal/golden"
	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	"github.com/tycheg/reasonr-compiler/internal/project"
	"github.com/tycheg/reasonr-compiler/internal/rfcblocks"
	"github.com/tycheg/reasonr-compiler/internal/sema"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/uigen"
)

// autoApps are the taxi apps whose UI the automatic UI builds.
const autoApps = `app(Staff, admin, support) Admin "Admin" auto
  language en
  default Customer.Id EntityPicker(search: Customer.FindByPhone(phoneNumber: text))

app(Customer) Taxi "Taxi" auto
  language en
  platforms web, mobile

app(dispatcher) Dispatch "Dispatch" auto

app(Driver) DriverApp "Driver" auto
  platforms mobile
`

// taxiWith is the taxi model without its layout and texts, with layout ui:
// apps go to apps/<App>/app.rr, screens to ui.rr of the bounded context
// whose model they bind to (the screen's context is screenCtx, Ordering by
// default).
func taxiWith(t *testing.T, ui string, screenCtx map[string]string) fstest.MapFS {
	t.Helper()
	dir := filepath.Join(rfcblocks.Examples(), "taxi")
	fsys := fstest.MapFS{}
	screens := map[string]*strings.Builder{}
	for _, decl := range strings.SplitAfter(strings.ReplaceAll(ui, "\n\n", "\n\n\x00"), "\x00") {
		decl = strings.TrimSuffix(decl, "\x00")
		if rest, ok := strings.CutPrefix(decl, "app("); ok {
			_, rest, _ = strings.Cut(rest, ") ")
			name, _, _ := strings.Cut(rest, " ")
			fsys["apps/"+name+"/app.rr"] = &fstest.MapFile{Data: []byte(decl)}
			continue
		}
		ctx := "Ordering"
		if rest, ok := strings.CutPrefix(decl, "screen "); ok {
			name, _, _ := strings.Cut(strings.TrimSpace(rest), "(")
			name, _, _ = strings.Cut(name, "\n")
			if c := screenCtx[strings.TrimSpace(name)]; c != "" {
				ctx = c
			}
		}
		if screens[ctx] == nil {
			screens[ctx] = &strings.Builder{}
		}
		screens[ctx].WriteString(decl)
	}
	for ctx, b := range screens {
		fsys["contexts/"+ctx+"/ui.rr"] = &fstest.MapFile{Data: []byte(b.String())}
	}
	files, err := filepath.Glob(filepath.Join(dir, "contexts", "*", "*.rr"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		switch filepath.Base(f) {
		case "ui.rr", "admin.rr", "texts.en.rr":
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(dir, f)
		fsys[filepath.ToSlash(rel)] = &fstest.MapFile{Data: data}
	}
	// The auth module (system Auth): its bounded contexts, without tests;
	// the taxi example requires it (reasonr mod download in examples/taxi).
	taxi := filepath.Join(filepath.Dir(rfcblocks.RFC()), "..", "examples", "taxi")
	pf, err := project.Read(taxi)
	if err != nil {
		t.Fatal(err)
	}
	deps, err := project.Deps(taxi, pf)
	if err != nil || len(deps) != 1 {
		t.Fatalf("the taxi modules: %v %v", deps, err)
	}
	mod := deps[0].Src
	files, err = filepath.Glob(filepath.Join(mod, "contexts", "*", "*.rr"))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, ".test.rr") || filepath.Base(f) == "ui.rr" {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		rel, _ := filepath.Rel(mod, f)
		fsys[filepath.ToSlash(rel)] = &fstest.MapFile{Data: data}
	}
	return fsys
}

func load(t *testing.T, fsys fstest.MapFS) *pipeline.Model {
	t.Helper()
	m, err := pipeline.OpenFS(source.NewFileSet(), fsys, "taxi")
	if err != nil {
		t.Fatal(err)
	}
	model, err := m.Model("")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	if diag.WriteText(&b, m.FileSet, model.Diagnostics); len(model.Diagnostics) > 0 {
		t.Fatalf("diagnostics:\n%s", b.String())
	}
	return model
}

// TestAuto: the app … auto layout for the taxi matches the golden file; the
// ejected layout compiles into the same UI program.
func TestAuto(t *testing.T) {
	auto := load(t, taxiWith(t, autoApps, nil))
	var ejected strings.Builder
	for _, name := range []string{"Admin", "Dispatch", "DriverApp", "Taxi"} { // the order of the apps/ directories
		a := auto.Info.UI.App(name)
		out, diags := format.Source(source.NewFileSet(), "auto.rr", a.AutoSource)
		if len(diags) > 0 {
			t.Fatalf("%s: the layout does not parse: %v", name, diags)
		}
		golden.Check(t, filepath.Join("testdata", "auto."+name+".rr"), out)
		ejected.Write(out)
		ejected.WriteByte('\n')
	}
	// A screen is ejected into the bounded context of the model it binds to.
	screenCtx := map[string]string{}
	for _, sc := range auto.Info.UI.Screens {
		screenCtx[sc.Decl.Short] = nodesContext(sc.Nodes)
	}
	written := load(t, taxiWith(t, ejected.String(), screenCtx))
	want := uigen.Generate(auto.Info)
	for i := range want.Apps {
		want.Apps[i].Auto = false
	}
	got := uigen.Generate(written.Info)
	// Screens are ordered by full name: ejected ones have a bounded context
	// in it, so they are compared by short name.
	short := func(name string) string { return name[strings.LastIndex(name, ".")+1:] }
	for _, u := range []*model.UI{want, got} {
		sort.Slice(u.Screens, func(i, j int) bool { return short(u.Screens[i].Name) < short(u.Screens[j].Name) })
	}
	a, _ := json.MarshalIndent(want, "", "  ")
	b, _ := json.MarshalIndent(got, "", "  ")
	// Ejected screens lie in a bounded context and are named in full; auto
	// screens are outside contexts: they are compared without the context name.
	strip := func(j []byte) string {
		return regexp.MustCompile(`"(Ordering|Dispatch)\.(\w+)"`).ReplaceAllStringFunc(string(j), func(m string) string {
			_, screen, _ := strings.Cut(strings.Trim(m, `"`), ".")
			if auto.Info.UI.Screen(screen) != nil {
				return `"` + screen + `"`
			}
			return m
		})
	}
	if sa, sb := strip(a), strip(b); sa != sb {
		la, lb := strings.Split(sa, "\n"), strings.Split(sb, "\n")
		for i := 0; i < len(la) && i < len(lb); i++ {
			if la[i] != lb[i] {
				t.Errorf("the ejected layout gives a different UI program: line %d: %s ≠ %s", i+1, la[i], lb[i])
				break
			}
		}
	}
}

// nodesContext is the bounded context of the first model whose methods the nodes bind to.
func nodesContext(nodes []*sema.Node) string {
	for _, n := range nodes {
		if n.Owner != nil && n.Owner.Context != "" {
			return n.Owner.Context
		}
		if c := nodesContext(n.Children); c != "" {
			return c
		}
	}
	return ""
}
