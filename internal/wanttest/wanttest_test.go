package wanttest

import (
	"reflect"
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/diag"
)

func TestParse(t *testing.T) {
	src := []byte("a = 1\nstatus = x  # want \"E0304\"\nb # want \"E0103\" \"W1002\"\n# wanted \"E0001\"\n")
	got := Parse(src)
	want := map[int][]diag.Code{
		2: {"E0304"},
		3: {"E0103", "W1002"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Parse = %v, want %v", got, want)
	}
}
