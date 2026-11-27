package vm

import (
	"context"
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/model"
)

// Benchmarks and the allocation check (plan, stage 7): typical commands and
// queries on a stub Env, the arena and the result from pools. Goals: 0
// allocations, a taxi command < 2 µs, a query with a search over 20 rows
// < 5 µs, engine time excluded.

// rowsEnv is a stub Env whose Find returns the first rows of the type
// without filtering or sorting: search time in the engine is not counted.
type rowsEnv struct{ *memEnv }

func (e rowsEnv) Find(_ context.Context, a *model.Arena, q *model.FindSpec) (model.Rows, error) {
	rows := e.rows[q.Type]
	n := min(len(rows), q.Size, 100)
	if !q.Page {
		n = min(len(rows), q.Limit)
	}
	m := a.Mark()
	for _, r := range rows[:n] {
		mr := a.Mark()
		for _, f := range e.a.Elems(r.state) {
			a.Push(a.Copy(e.a, f))
		}
		a.Push(a.Copy(e.a, r.id))
		a.Push(a.End(model.KindRecord, q.Type, mr))
	}
	return model.Rows{Records: a.End(model.KindList, 0, m), Next: model.None}, nil
}

// work is a typical unit of work or query for a benchmark and the allocation check.
type work struct {
	name string
	run  func() error
}

func typicalWork(t *testing.T) []work {
	var out []work
	ctx := context.Background()
	command := func(name string, f *fixture, env model.Env, call model.CommandCall) {
		call.Arena, call.Now = f.env.a, f.now
		out = append(out, work{name, func() error {
			res, err := f.m.Execute(ctx, call, env)
			if err == nil && res.Work == nil {
				t.Fatalf("%s: errors %v", name, res.Errors)
			}
			if err == nil {
				res.Release()
			}
			return err
		}})
	}
	query := func(name string, f *fixture, env model.Env, call model.QueryCall) {
		call.Arena, call.Now = f.env.a, f.now
		out = append(out, work{name, func() error {
			res, err := f.m.Query(ctx, call, env)
			if err == nil && len(res.Errors) > 0 {
				t.Fatalf("%s: errors", name)
			}
			if err == nil {
				res.Release()
			}
			return err
		}})
	}

	bank := load(t, example("bank"), "")
	a := bank.env.a
	c1 := bank.id("Client", 1)
	bank.put("Account", bank.id("Account", 1), map[string]model.Value{"owner": c1, "overdraft": decimal("1000000"), "balance": decimal("5")})
	bank.put("Account", bank.id("Account", 2), map[string]model.Value{"owner": c1})
	acc := bank.typ("Account")
	command("Account.Transfer", bank, bank.env, model.CommandCall{Entity: acc, ID: bank.id("Account", 1),
		Command: bank.s.Type(acc).Method("Transfer").Ref, Args: a.Record(0, bank.id("Account", 2), decimal("1"), a.String("")),
		Caller: bank.actor("Client", c1)})
	query("Account.Details", bank, bank.env, model.QueryCall{Type: acc, Query: bank.s.Type(acc).Method("Details").Ref,
		Args: a.Record(0, bank.id("Account", 1)), Caller: bank.actor("Client", c1)})

	taxi := load(t, example("taxi"), "")
	a = taxi.env.a
	route := taxiData(taxi)
	cust, drv := taxi.typ("Customer"), taxi.typ("Driver")
	command("Customer.OrderTaxi", taxi, taxi.env, model.CommandCall{Entity: cust, ID: taxi.id("Customer", 1),
		Command: taxi.s.Type(cust).Method("OrderTaxi").Ref, Caller: taxi.actor("Customer", taxi.id("Customer", 1)),
		Args: a.Record(0, taxi.id("Order", 1), route, taxi.enum("Ordering.CarClass", "economy"))})
	d3, o3, t3 := taxi.id("Driver", 3), taxi.id("Order", 3), taxi.id("Trip", 3)
	taxi.put("Driver", d3, map[string]model.Value{"name": a.String("Oliver"), "status": taxi.enum("DriverStatus", "offered"),
		"currentTrip": t3, "car": taxi.value(a, "Car", map[string]model.Value{"plate": a.String("C003CC77")})})
	taxi.put("Order", o3, map[string]model.Value{"status": taxi.enum("OrderStatus", "offered"),
		"customer": taxi.id("Customer", 1)})
	taxi.put("Trip", t3, map[string]model.Value{"stage": taxi.enum("TripStage", "offered"), "candidate": d3,
		"offerTimer": model.TimerID([16]byte{15: 9})})
	command("Driver.Accept", taxi, taxi.env, model.CommandCall{Entity: drv, ID: d3,
		Command: taxi.s.Type(drv).Method("Accept").Ref, Caller: taxi.actor("Driver", d3), Args: a.Record(0)})

	// A search: 20 free drivers, a page of 20.
	for i := byte(10); i < 30; i++ {
		taxi.put("Driver", taxi.id("Driver", i), map[string]model.Value{"name": a.String("Driver"),
			"status": taxi.enum("DriverStatus", "free"), "car": taxi.value(a, "Car", map[string]model.Value{"plate": a.String("E")})})
	}
	m := a.Mark()
	a.Push(model.Int(20))
	a.Push(model.None)
	page := a.End(model.KindPageRequest, 0, m)
	query("Driver.FreeDrivers", taxi, rowsEnv{taxi.env}, model.QueryCall{Type: drv, Query: taxi.s.Type(drv).Method("FreeDrivers").Ref,
		Args: a.Record(0, taxi.enum("Dispatch.CarClass", "economy"), page), Caller: taxi.caller(role("dispatcher"))})
	return out
}

// TestAllocs: typical commands and queries in steady state do not allocate
// (the stage 7 criterion, checked in CI).
func TestAllocs(t *testing.T) {
	if raceEnabled {
		t.Skip("sync.Pool loses objects under -race")
	}
	for _, w := range typicalWork(t) {
		for range 3 { // warm up the pools
			if err := w.run(); err != nil {
				t.Fatalf("%s: %v", w.name, err)
			}
		}
		if n := testing.AllocsPerRun(100, func() { _ = w.run() }); n != 0 {
			t.Errorf("%s: %.1f allocations, expected 0", w.name, n)
		}
	}
}

func BenchmarkTypical(b *testing.B) {
	for _, w := range typicalWork(&testing.T{}) {
		b.Run(w.name, func(b *testing.B) {
			b.ReportAllocs()
			for range b.N {
				if err := w.run(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
