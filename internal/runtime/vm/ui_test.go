package vm_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	"github.com/tycheg/reasonr-compiler/internal/rfcblocks"
	"github.com/tycheg/reasonr-compiler/internal/runtime/vm"
	"github.com/tycheg/reasonr-compiler/internal/source"
)

// TestEval: if conditions of the taxi screens are evaluated by scope values.
func TestEval(t *testing.T) {
	m, err := pipeline.Load(source.NewFileSet(), filepath.Join(rfcblocks.Examples(), "taxi"), "")
	if err != nil || m.HasErrors() {
		t.Fatal(err, m.Diagnostics)
	}
	bc, err := pipeline.Build(m)
	if err != nil {
		t.Fatal(err)
	}
	mach, err := vm.New(bc, vm.DefaultLimits)
	if err != nil {
		t.Fatal(err)
	}
	s := mach.Schema()
	status := func(name string) model.Value {
		t.Helper()
		if ti := s.Lookup("DriverStatus"); ti != nil {
			for j, v := range ti.Enum {
				if v == name {
					return model.Enum(ti.Ref, j)
				}
			}
		}
		t.Fatalf("no value %s", name)
		return model.None
	}
	a := model.GetArena()
	defer model.PutArena(a)
	for _, c := range []struct {
		fn, status string
		want       bool
	}{
		{"ui.Dispatch.Shift:Driver.GoOnline.if", "offline", true},
		{"ui.Dispatch.Shift:Driver.GoOnline.if", "free", false},
		{"ui.Dispatch.Shift:Driver.GoOffline.if", "free", true},
	} {
		v, err := mach.Eval(context.Background(), c.fn, []model.Value{status(c.status)}, a, model.Caller{}, time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if v != model.Bool(c.want) {
			t.Errorf("%s(%s) = %v, want %v", c.fn, c.status, v, c.want)
		}
	}
	if _, err := mach.Eval(context.Background(), "ui.Nope", nil, a, model.Caller{}, time.Now()); err == nil {
		t.Error("no error for an unknown expression")
	}
}
