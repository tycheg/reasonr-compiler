// Examples of RFC-0001. The RFC is the source of truth: `go test . -update`
// in docs/ rewrites the example files from the ```rr blocks with a path heading.
package docs_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/tycheg/reasonr-compiler/docs/internal/golden"
	"github.com/tycheg/reasonr-compiler/docs/internal/rfcblocks"
)

const rfcPath = "rfc/0001-reasonr-language"

// copies are the directories the RFC models are written to: the header
// "# taxi/contexts/Urcar.Ordering/order.rr" is the file
// compiler/examples/taxi/src/contexts/Urcar.Ordering/order.rr.
var copies = []struct {
	root string                      // directory relative to docs/
	path func(ex, rel string) string // path of the module file in root
}{
	{"../examples", func(ex, rel string) string { return filepath.Join(ex, "src", rel) }},
}

// TestRFCSync checks that the example models match the RFC blocks and have
// no *.rr files missing from the RFC.
func TestRFCSync(t *testing.T) {
	blocks, err := rfcblocks.Load(rfcPath)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, b := range blocks {
		if b.File == "" {
			continue
		}
		if seen[b.File] {
			t.Errorf("%s: file %s appears in the RFC twice", b.Pos(), b.File)
		}
		seen[b.File] = true
	}
	if len(seen) == 0 {
		t.Fatal("the RFC has no blocks with a path heading")
	}
	for _, c := range copies {
		dirs := map[string]bool{} // example model directories in root
		inRFC := map[string]bool{}
		for _, b := range blocks {
			if b.File == "" {
				continue
			}
			ex, rel, _ := strings.Cut(b.File, "/")
			p := filepath.Join(c.root, c.path(ex, filepath.FromSlash(rel)))
			inRFC[p] = true
			dirs[filepath.Join(c.root, c.path(ex, ""))] = true
			golden.Check(t, p, []byte(b.Text))
		}
		var extra []string
		for dir := range dirs {
			if err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() && d.Name() == "node_modules" {
					return filepath.SkipDir
				}
				if !d.IsDir() && strings.HasSuffix(path, ".rr") && !inRFC[path] {
					extra = append(extra, path)
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		}
		sort.Strings(extra)
		for _, p := range extra {
			if golden.Updating() {
				if err := os.Remove(p); err != nil {
					t.Error(err)
				}
				continue
			}
			t.Errorf("%s: the file is not in the RFC", p)
		}
	}
}
