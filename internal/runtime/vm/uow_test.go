package vm

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/golden"
	"github.com/tycheg/reasonr-compiler/internal/model"
)

// dump prints a command result: errors or a unit of work with schema names.
func (f *fixture) dump(out *model.Outcome) string {
	a := out.Arena
	r := &run{m: f.m, a: a}
	val := func(v model.Value) string {
		s := a.StringStart()
		r.text(v)
		return a.Text(a.StringEnd(s))
	}
	// args prints an arguments record with the method's parameter names.
	args := func(t model.TypeRef, m model.MethodRef, rec model.Record) string {
		mi := &f.s.Type(t).Methods[m]
		var parts []string
		for i, v := range a.Fields(rec) {
			parts = append(parts, mi.Params[i].Name+": "+val(v))
		}
		return f.s.Type(t).Name + "." + mi.Name + "(" + strings.Join(parts, ", ") + ")"
	}
	var b strings.Builder
	for _, e := range out.Errors {
		if e.Owner == 0 {
			fmt.Fprintf(&b, "error %s%s\n", e.Name, val(e.Args.Value()))
			continue
		}
		fmt.Fprintf(&b, "error %s\n", args(e.Owner, f.s.Type(e.Owner).Method(e.Name).Ref, e.Args))
	}
	w := out.Work
	if w == nil {
		return b.String()
	}
	for _, e := range w.Entities {
		fmt.Fprintf(&b, "entity %s %s existed=%v version=%d\n", f.s.Type(e.Type).Name, val(e.ID), e.Existed, e.Version)
		for _, ev := range e.Events {
			fmt.Fprintf(&b, "  #%d event %s\n", ev.Seq, args(e.Type, ev.Name, ev.Args))
		}
		fmt.Fprintf(&b, "  state %s\n", val(e.State.Value()))
	}
	for _, p := range w.Projections {
		name := f.s.Type(p.Projection).Name
		if p.Kind == model.ProjectionDelete {
			fmt.Fprintf(&b, "#%d projection delete %s %s\n", p.Seq, name, val(p.ID))
			continue
		}
		var set []string
		for _, fv := range p.Set {
			set = append(set, f.s.Type(p.Projection).Fields[fv.Field].Name+": "+val(fv.Value))
		}
		fmt.Fprintf(&b, "#%d projection upsert %s %s {%s}\n", p.Seq, name, val(p.ID), strings.Join(set, ", "))
	}
	for _, t := range w.Timers {
		if t.Kind == model.TimerDelete {
			fmt.Fprintf(&b, "timer delete %s\n", val(t.ID))
			continue
		}
		fmt.Fprintf(&b, "timer %s at %s: [%s].%s\n", val(t.ID), t.At.Format(time.RFC3339), val(t.EntityID), args(t.Entity, t.Command, t.Args))
	}
	for _, n := range w.Notifications {
		to := "initiator"
		if n.To.Kind != model.KindNone {
			to = val(n.To)
		}
		fmt.Fprintf(&b, "notify %s to %s\n", args(n.Owner, n.Name, n.Args), to)
	}
	for _, q := range w.Requests {
		fmt.Fprintf(&b, "request %s key %s\n", args(q.System, q.Request, q.Args), val(q.Key))
	}
	for _, s := range w.Forget {
		fmt.Fprintf(&b, "forget %s %s\n", f.s.Type(s.Type).Name, val(s.ID))
	}
	return b.String()
}

// exec calls command typ.name of entity id.
func (f *fixture) exec(typ string, id model.Value, name string, c model.Caller, args ...model.Value) (*model.Outcome, error) {
	ti, mi := f.s.Method(typ, name)
	if mi == nil {
		f.t.Fatalf("no command %s.%s", typ, name)
	}
	t := ti.Ref
	c = f.caller(c)
	return f.m.Execute(context.Background(), model.CommandCall{
		Entity: t, ID: id, Command: mi.Ref, Args: f.env.a.Record(0, args...), Arena: f.env.a, Caller: c, Now: f.now,
	}, f.env)
}

// scenario runs a command and checks the dump against golden file testdata/uow/name.
func (f *fixture) scenario(name, typ string, id model.Value, cmd string, c model.Caller, args ...model.Value) {
	f.t.Helper()
	out, err := f.exec(typ, id, cmd, c, args...)
	if err != nil {
		f.t.Fatalf("%s: failure: %v", name, err)
	}
	golden.Check(f.t, filepath.Join("testdata", "uow", name+".txt"), []byte(f.dump(out)))
	out.Release()
}

var timer = model.Caller{Kind: model.CallerTimer}

// taxiData: customer c1, drivers d1 (free) and d2 (busy), a route.
func taxiData(f *fixture) (route model.Value) {
	a := f.env.a
	point := func(lat, lon float64) model.Value {
		return f.value(a, "Ordering.GeoPoint", map[string]model.Value{"lat": model.Float(lat), "lon": model.Float(lon)})
	}
	car := func(plate string) model.Value {
		return f.value(a, "Car", map[string]model.Value{"plate": a.String(plate), "model": a.String("Kia Rio"),
			"carClass": f.enum("Dispatch.CarClass", "economy")})
	}
	f.put("Customer", f.id("Customer", 1), map[string]model.Value{"phone": a.String("+79990000000")})
	f.put("Driver", f.id("Driver", 1), map[string]model.Value{"name": a.String("Ivan"), "car": car("A001AA77"),
		"status": f.enum("DriverStatus", "free")})
	f.put("Driver", f.id("Driver", 2), map[string]model.Value{"name": a.String("Peter"), "car": car("B002BB77"),
		"status": f.enum("DriverStatus", "busy"), "currentTrip": f.id("Trip", 9)})
	return f.value(a, "Route", map[string]model.Value{"from": point(55.75, 37.62), "to": point(55.8, 37.6),
		"distanceKm": model.Float(3)})
}

func TestTaxiCommands(t *testing.T) {
	f := load(t, example("taxi"), "")
	a := f.env.a
	route := taxiData(f)
	c1 := f.id("Customer", 1)
	economy := f.enum("Ordering.CarClass", "economy")

	f.scenario("signup", "Customer", f.id("Customer", 5), "SignUp", f.actor("Customer", f.id("Customer", 5)), a.String("+79991112233"))
	f.scenario("signup-twice", "Customer", c1, "SignUp", f.actor("Customer", c1), a.String("+7"))
	f.scenario("order-taxi", "Customer", c1, "OrderTaxi", f.actor("Customer", c1), f.id("Order", 1), route, economy)
	f.scenario("order-taxi-forbidden", "Customer", c1, "OrderTaxi", f.actor("Customer", f.id("Customer", 2)), f.id("Order", 1), route, economy)
	bad := f.value(a, "Route", map[string]model.Value{
		"from": f.value(a, "Ordering.GeoPoint", map[string]model.Value{"lat": model.Float(100)}),
		"to":   f.value(a, "Ordering.GeoPoint", nil), "distanceKm": model.Float(1)})
	f.scenario("order-taxi-invalid", "Customer", c1, "OrderTaxi", f.actor("Customer", c1), f.id("Order", 1), bad, economy)
	f.scenario("forget", "Customer", c1, "Forget", f.actor("Customer", c1))

	// A trip in search: driver d2 is busy, a refusal returns the trip to the
	// search (a reentrant call Trip → Driver → Trip).
	o2, t2 := f.id("Order", 2), f.id("Trip", 2)
	price := f.value(a, "Ordering.Money", map[string]model.Value{"amount": decimal("190"), "currency": f.enum("Ordering.Currency", "rub")})
	f.put("Order", o2, map[string]model.Value{"status": f.enum("OrderStatus", "searching"), "customer": c1,
		"route": route, "price": price})
	// A Dispatch trip: picking a driver.
	fare := f.value(a, "Dispatch.Money", map[string]model.Value{"amount": decimal("190"), "currency": f.enum("Dispatch.Currency", "rub")})
	pickup := f.value(a, "Dispatch.GeoPoint", map[string]model.Value{"lat": model.Float(55.75), "lon": model.Float(37.62)})
	f.put("Trip", t2, map[string]model.Value{"stage": f.enum("TripStage", "searching"),
		"pickup": pickup, "searchTimer": model.TimerID([16]byte{15: 7}), "fare": fare})
	f.scenario("driver-busy", "Trip", t2, "DriverFound", role("matching"), f.id("Driver", 2))
	f.scenario("driver-found", "Trip", t2, "DriverFound", role("matching"), f.id("Driver", 1))
	f.scenario("search-timed-out", "Trip", t2, "searchTimedOut", timer)
	f.scenario("private-from-outside", "Trip", t2, "searchTimedOut", role("matching"))
}

func TestExtensionReaction(t *testing.T) {
	f := load(t, example("taxi"), "acme")
	route := taxiData(f)
	c1 := f.id("Customer", 1)
	f.scenario("acme-order-taxi", "Customer", c1, "OrderTaxi", f.actor("Customer", c1), f.id("Order", 1), route, f.enum("Ordering.CarClass", "economy"))
	far := f.value(f.env.a, "Route", map[string]model.Value{"from": f.value(f.env.a, "Ordering.GeoPoint", nil),
		"to": f.value(f.env.a, "Ordering.GeoPoint", nil), "distanceKm": model.Float(1000)})
	f.scenario("acme-order-limit", "Customer", c1, "OrderTaxi", f.actor("Customer", c1), f.id("Order", 1), far, f.enum("Ordering.CarClass", "economy"))
}

func TestBankCommands(t *testing.T) {
	f := load(t, example("bank"), "")
	a := f.env.a
	c1, c2 := f.id("Client", 1), f.id("Client", 2)
	acc := func(n byte) model.Value { return f.id("Account", n) }
	f.put("Client", c1, map[string]model.Value{"fullName": a.String("Anna")})
	f.put("Account", acc(1), map[string]model.Value{"owner": c1, "balance": decimal("100"), "overdraft": decimal("50")})
	f.put("Account", acc(2), map[string]model.Value{"owner": c2, "balance": decimal("10")})
	f.put("Account", acc(3), map[string]model.Value{"owner": c2, "frozen": model.Bool(true)})

	f.scenario("transfer", "Account", acc(1), "Transfer", f.actor("Client", c1), acc(2), decimal("120"), a.String("for lunch"))
	f.scenario("transfer-insufficient", "Account", acc(1), "Transfer", f.actor("Client", c1), acc(2), decimal("151"), a.String(""))
	f.scenario("transfer-frozen-target", "Account", acc(1), "Transfer", f.actor("Client", c1), acc(3), decimal("1"), a.String(""))
	f.scenario("transfer-validation", "Account", acc(1), "Transfer", f.actor("Client", c1), acc(1), decimal("-1"), a.String(strings.Repeat("x", 141)))
	f.scenario("open-account", "Account", acc(7), "Open", f.actor("Client", c1))
	f.scenario("join", "Client", f.id("Client", 3), "Join", f.actor("Client", f.id("Client", 3)), a.String("Boris"))
}

func TestConstructsCommands(t *testing.T) {
	f := load(t, "../../lower/testdata/constructs", "")
	a := f.env.a
	b1, k1 := f.id("Box", 1), f.id("Keeper", 1)
	admin := model.Caller{Kind: model.CallerUser, Roles: []string{"admin"}}
	f.scenario("box-create", "Box", f.id("Box", 2), "Create", admin, k1)
	f.put("Keeper", k1, map[string]model.Value{"name": a.String("Keeper")})
	m := a.Mark()
	a.Push(a.String("a"))
	a.Push(model.Int(2))
	counts := a.End(model.KindMap, 0, m)
	f.put("Box", b1, map[string]model.Value{"kind": f.enum("Kind", "small"), "owner": k1, "counts": counts,
		"items": a.List(a.String("a")), "timer": model.TimerID([16]byte{15: 3})})
	names := a.List(a.String("x"), a.String(""), a.String("y"))
	f.scenario("box-fill", "Box", b1, "Fill", admin, names)
	f.scenario("box-fill-limit", "Box", b1, "Fill", admin, names, model.Int(1))
	f.scenario("box-expire", "Box", b1, "expire", timer)
	f.scenario("keeper-leave", "Keeper", k1, "Leave", f.actor("Keeper", k1))
}

func TestCommandFaults(t *testing.T) {
	f := load(t, example("bank"), "")
	c1 := f.id("Client", 1)
	f.put("Account", f.id("Account", 1), map[string]model.Value{"owner": c1})
	if _, err := f.exec("Account", f.id("Client", 1), "Transfer", f.actor("Client", c1)); !errors.Is(err, ErrNotFound) {
		t.Errorf("an identifier of another type: %v", err)
	}
	f.m.limits.Events = 1
	out, err := f.exec("Account", f.id("Account", 1), "Transfer", f.actor("Client", c1), f.id("Account", 2), decimal("-1"), f.env.a.String(""))
	if err != nil || len(out.Errors) == 0 {
		t.Errorf("errors before events do not hit the limit: %v", err)
	}
	f.put("Account", f.id("Account", 2), map[string]model.Value{"owner": c1})
	f.put("Account", f.id("Account", 3), map[string]model.Value{"owner": c1, "overdraft": decimal("10")})
	if _, err := f.exec("Account", f.id("Account", 3), "Transfer", f.actor("Client", c1), f.id("Account", 2), decimal("1"), f.env.a.String("")); !errors.Is(err, ErrLimit) {
		t.Errorf("event limit: %v", err)
	}
	f.m.limits = DefaultLimits
	f.env.fail = errors.New("no connection")
	if _, err := f.exec("Account", f.id("Account", 1), "Freeze", role("compliance"), f.env.a.String("")); err == nil {
		t.Error("the Env failure did not come back")
	}
}

// TestDeterminism: repeating a command gives the same unit of work.
func TestDeterminism(t *testing.T) {
	f := load(t, example("taxi"), "")
	route := taxiData(f)
	c1 := f.id("Customer", 1)
	var dumps []string
	for range 2 {
		f.env.ids = 0
		out, err := f.exec("Customer", c1, "OrderTaxi", f.actor("Customer", c1), f.id("Order", 1), route, f.enum("Ordering.CarClass", "economy"))
		if err != nil {
			t.Fatal(err)
		}
		dumps = append(dumps, f.dump(out))
		out.Release()
	}
	if dumps[0] != dumps[1] {
		t.Errorf("a repeat gave a different unit of work:\n%s\n---\n%s", dumps[0], dumps[1])
	}
}
