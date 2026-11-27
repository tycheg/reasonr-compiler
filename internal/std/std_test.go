package std_test

import (
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/format"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/std"
)

// TestFormatted: the catalog parses without errors and is already formatted.
func TestFormatted(t *testing.T) {
	out, diags := format.Source(source.NewFileSet(), std.UIName, std.UI)
	for _, d := range diags {
		t.Errorf("%v", d)
	}
	if string(out) != string(std.UI) {
		t.Errorf("the catalog is not formatted:\n%s", out)
	}
}
