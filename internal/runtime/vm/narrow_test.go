package vm

import (
	"strings"
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/model"
)

// TestNarrowing: inside if x is not none an optional value is a value of a
// non-optional type for the operations that require one (add.str, add.i64).
func TestNarrowing(t *testing.T) {
	f := load(t, "testdata/narrow", "")
	a := f.env.a
	b1, b2 := f.id("Box", 1), f.id("Box", 2)
	label := f.value(a, "Label", map[string]model.Value{"text": a.String("tag")})
	f.put("Box", b1, map[string]model.Value{"note": a.String("note"), "size": model.Int(41), "label": label})
	f.put("Box", b2, map[string]model.Value{})
	texts := func(id model.Value, extra model.Value) string {
		out, err := f.exec("Box", id, "Check", role("admin"), extra)
		if err != nil {
			t.Fatal(err)
		}
		defer out.Release()
		var ts []string
		for _, e := range out.Errors {
			ts = append(ts, out.Arena.Text(out.Arena.Elems(e.Args.Value())[0]))
		}
		return strings.Join(ts, " | ")
	}
	if got := texts(b1, a.String("extra")); got != "xnote | n42 | cnote | eextra | ltag | ttag" {
		t.Errorf("with data: %s", got)
	}
	if got := texts(b2, model.None); got != "" {
		t.Errorf("without data: %s", got)
	}
}
