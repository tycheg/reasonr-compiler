package vm

import (
	"fmt"
	"strings"
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/model"
)

// TestArgSubjects: the personal data subjects of the arguments of requests,
// notifications and errors are computed by paths from self
// (RFC §20).
func TestArgSubjects(t *testing.T) {
	f := load(t, "testdata/pii", "")
	a := f.env.a
	str := func(s string) model.Value { return a.String(s) }
	c1, o1, o2 := f.id("Customer", 1), f.id("Order", 1), f.id("Order", 2)
	subjects := func(ss []model.ArgSubject, params []model.FieldInfo) string {
		var out []string
		for _, s := range ss {
			out = append(out, fmt.Sprintf("%s=%s:%d", params[s.Arg].Name, f.s.Type(s.Subject.Type).Name, s.Subject.ID.UUID()[15]))
		}
		return strings.Join(out, " ")
	}
	method := func(t model.TypeRef, m model.MethodRef) []model.FieldInfo { return f.s.Type(t).Methods[m].Params }

	out, err := f.exec("Customer", c1, "SignUp", f.actor("Customer", c1), str("+79990001122"))
	if err != nil {
		t.Fatal(err)
	}
	w := out.Work
	if got := subjects(w.Notifications[0].Subjects, method(w.Notifications[0].Owner, w.Notifications[0].Name)); got != "text=Customer:1" {
		t.Errorf("notification: %s", got)
	}
	if got := subjects(w.Requests[0].Subjects, method(w.Requests[0].System, w.Requests[0].Request)); got != "to=Customer:1" {
		t.Errorf("SignUp request: %s", got)
	}
	out.Release()

	f.put("Order", o1, map[string]model.Value{"customer": c1, "address": str("1 Main St"), "note": str("intercom 12")})
	out, err = f.exec("Order", o1, "Dispatch", role("admin"))
	if err != nil {
		t.Fatal(err)
	}
	w = out.Work
	if got := subjects(w.Requests[0].Subjects, method(w.Requests[0].System, w.Requests[0].Request)); got != "comment=Order:1 where=Customer:1" {
		t.Errorf("Dispatch request: %s", got)
	}
	out.Release()

	ti := f.s.Type(f.typ("Order"))
	out, err = f.exec("Order", o1, "Check", role("admin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Errors) != 1 || subjects(out.Errors[0].Subjects, ti.Method("badNote").Params) != "both=Customer:1 both=Order:1" {
		t.Errorf("error: %+v", out.Errors)
	}
	out.Release()

	// A none subject (an order without a customer) is skipped.
	f.put("Order", o2, map[string]model.Value{"note": str("x")})
	out, err = f.exec("Order", o2, "Check", role("admin"))
	if err != nil {
		t.Fatal(err)
	}
	if got := subjects(out.Errors[0].Subjects, ti.Method("badNote").Params); got != "both=Order:2" {
		t.Errorf("without a customer: %s", got)
	}
	out.Release()
}
