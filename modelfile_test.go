package reasonr_test

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	reasonr "github.com/tycheg/reasonr-compiler"
)

// TestModelFile: a model read from its model file answers like the model
// built from the sources: the schema, the interface, the scenarios, the
// performance checks, the files, the compatibility; its tests run.
func TestModelFile(t *testing.T) {
	built, err := reasonr.Build(filepath.Join("examples", "taxi", "src"), reasonr.Options{Extension: "acme", Test: true})
	if err != nil {
		t.Fatal(err)
	}
	data, err := built.Encode()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "taxi.rrm")
	if err := writeFile(path, data); err != nil {
		t.Fatal(err)
	}
	opened, err := reasonr.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name      string
		got, want any
	}{
		{"extension", opened.Extension(), built.Extension()},
		{"test", opened.Test(), built.Test()},
		{"schema", opened.Schema(), built.Schema()},
		{"ui", opened.UI(), built.UI()},
		{"scenarios", opened.Scenarios(), built.Scenarios()},
		{"tests", opened.Tests(), built.Tests()},
		{"files", opened.Files(), built.Files()},
		{"hash", opened.SourceHash(), built.SourceHash()},
		{"reads", opened.CommandReads(), built.CommandReads()},
		{"primitives", opened.Primitives("web"), built.Primitives("web")},
		{"manifest", opened.Manifest("Admin", "en", "web"), built.Manifest("Admin", "en", "web")},
	} {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s differs after the model file", c.name)
		}
	}
	if len(opened.Scenarios()) == 0 {
		t.Fatal("no scenarios")
	}
	if opened.Compatible(built.Schema()) != nil {
		t.Error("the model is incompatible with its own schema")
	}
	bad := filepath.Join(t.TempDir(), "bad.model")
	if err := writeFile(bad, []byte("not a model")); err != nil {
		t.Fatal(err)
	}
	if _, err := reasonr.Open(bad); err == nil {
		t.Error("a file without the header is read")
	}
}

func writeFile(path string, data []byte) error { return os.WriteFile(path, data, 0o644) }
