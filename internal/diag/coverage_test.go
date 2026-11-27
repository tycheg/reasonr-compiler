package diag_test

import (
	"path/filepath"
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/diag"
	"github.com/tycheg/reasonr-compiler/internal/wanttest"
)

// TestRegistryCoverage checks that every code is covered by a `# want`
// test and that tests do not refer to codes outside the registry.
func TestRegistryCoverage(t *testing.T) {
	covered, err := wanttest.ScanCodes(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range diag.All() {
		if len(covered[s.Code]) == 0 {
			t.Errorf("%s (%s): no # want test", s.Code, s.Title)
		}
	}
	for c, files := range covered {
		if diag.Lookup(c) == nil {
			t.Errorf("%s: the code is not in the registry but is expected in %v", c, files)
		}
	}
}
