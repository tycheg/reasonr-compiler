// Package genmodel generates a synthetic model for scale measurements
// (docs/plans/0015): bounded contexts with aggregates, commands, events,
// calls, index queries, ACLs between contexts, screens, apps and tests. The
// model has no errors and is deterministic by seed.
package genmodel

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"
)

// Options are the size and spread of the model.
type Options struct {
	Lines int    // *.rr lines, at least
	Seed  uint64 // seed of the size spread
	// UIShare is the share of bounded contexts with screens and an app (0.2 by default).
	UIShare float64
}

// Stats is what came out.
type Stats struct {
	Lines      int `json:"lines"`
	Files      int `json:"files"`
	Contexts   int `json:"contexts"`
	Aggregates int `json:"aggregates"`
	Commands   int `json:"commands"`
	Tests      int `json:"tests"`
	Apps       int `json:"apps"`
}

// ParseLines parses a size: 100k, 1m, 10m or a number.
func ParseLines(s string) (int, error) {
	mul := 1
	switch {
	case strings.HasSuffix(s, "k"):
		mul, s = 1_000, strings.TrimSuffix(s, "k")
	case strings.HasSuffix(s, "m"):
		mul, s = 1_000_000, strings.TrimSuffix(s, "m")
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("size %q: expected a number, 100k or 10m", s)
	}
	return n * mul, nil
}

// Generate writes the model into directory dir (module sources: contexts/, apps/).
func Generate(dir string, o Options) (Stats, error) {
	if o.UIShare == 0 {
		o.UIShare = 0.2
	}
	rnd := rand.New(rand.NewPCG(o.Seed, 0x5ca1e))
	g := &gen{dir: dir}
	for c := 0; g.st.Lines < o.Lines; c++ {
		ctx := context{Name: fmt.Sprintf("C%04d", c), N: c, Aggs: 4 + rnd.IntN(9)}
		if c > 0 {
			ctx.Prev = fmt.Sprintf("C%04d", c-1)
		}
		ctx.UI = rnd.Float64() < o.UIShare
		if err := g.context(ctx); err != nil {
			return g.st, err
		}
	}
	return g.st, nil
}

type gen struct {
	dir string
	st  Stats
}

type context struct {
	Name, Prev string
	N, Aggs    int
	UI         bool
}

type aggregate struct {
	Ctx  string
	Name string // Item3
	Prev string // Item2: a reference and a call; "" for the first
	J    int
	P    string // prefix of notifications and errors: c0007i3 — names are unique in the project (tests see them)
	Last bool   // the last one in the bounded context: nothing touches it
	// Mirror is a copy from a neighboring bounded context through an ACL (mirror).
	Mirror bool
}

func (g *gen) write(rel string, data []byte) error {
	p := filepath.Join(g.dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	g.st.Files++
	g.st.Lines += bytes.Count(data, []byte("\n"))
	return os.WriteFile(p, data, 0o644)
}

func (g *gen) context(c context) error {
	g.st.Contexts++
	base := "contexts/" + c.Name + "/"
	if err := g.write(base+"types.rr", render(typesTmpl, c)); err != nil {
		return err
	}
	var tests bytes.Buffer
	for j := 0; j < c.Aggs; j++ {
		a := aggregate{Ctx: c.Name, Name: fmt.Sprintf("Item%d", j), J: j, P: fmt.Sprintf("c%04di%d", c.N, j), Last: j == c.Aggs-1, Mirror: j == 0 && c.Prev != ""}
		if j > 0 {
			a.Prev = fmt.Sprintf("Item%d", j-1)
		}
		g.st.Aggregates++
		g.st.Commands += 7
		for _, has := range []bool{a.Prev != "", !a.Last, a.Mirror} {
			if has {
				g.st.Commands++
			}
		}
		if err := g.write(base+strings.ToLower(a.Name)+".rr", render(aggTmpl, a)); err != nil {
			return err
		}
		tests.Write(render(testTmpl, a))
		g.st.Tests += 4
	}
	if err := g.write(base+"items.test.rr", append([]byte("# "+base+"items.test.rr\n"), tests.Bytes()...)); err != nil {
		return err
	}
	if c.Prev != "" {
		if err := g.write(base+strings.ToLower(c.Prev)+".acl.rr", render(aclTmpl, c)); err != nil {
			return err
		}
		if err := g.write(base+"acl.test.rr", render(aclTestTmpl, c)); err != nil {
			return err
		}
		g.st.Tests++
	}
	if c.UI {
		g.st.Apps++
		if err := g.write(base+"ui.rr", render(uiTmpl, c)); err != nil {
			return err
		}
		if err := g.write("apps/App"+c.Name+"/app.rr", render(appTmpl, c)); err != nil {
			return err
		}
	}
	return nil
}

func render(t *template.Template, data any) []byte {
	var b bytes.Buffer
	if err := t.Execute(&b, data); err != nil {
		panic(err)
	}
	return b.Bytes()
}

var funcs = template.FuncMap{"lower": strings.ToLower}

func tmpl(text string) *template.Template {
	return template.Must(template.New("").Funcs(funcs).Parse(strings.TrimLeft(text, "\n")))
}
