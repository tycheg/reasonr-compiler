package vm

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/bytecode"
	"github.com/tycheg/reasonr-compiler/internal/runtime/dec"
)

func decimal(s string) model.Value { return model.Decimal(dec.MustParse(s)) }

// pageReq is PageRequest(size, cursor) in arena a.
func pageReq(a *model.Arena, size int64, cursor string) model.Value {
	m := a.Mark()
	a.Push(model.Int(size))
	if cursor == "" {
		a.Push(model.None)
	} else {
		a.Push(a.String(cursor))
	}
	return a.End(model.KindPageRequest, 0, m)
}

func (f *fixture) expect(out *model.QueryOutcome, err error, want string) {
	f.t.Helper()
	if err != nil {
		f.t.Fatalf("failure: %v", err)
	}
	if got := f.show(out); got != want {
		f.t.Errorf("result:\n  %s\nexpected:\n  %s", got, want)
	}
	out.Release()
}

func TestBankQueries(t *testing.T) {
	f := load(t, example("bank"), "")
	a := f.env.a
	c1, c2 := f.id("Client", 1), f.id("Client", 2)
	acc := func(n byte) model.Value { return f.id("Account", n) }
	f.put("Account", acc(1), map[string]model.Value{"owner": c1, "balance": decimal("100.5")})
	f.put("Account", acc(2), map[string]model.Value{"owner": c2, "balance": decimal("7")})
	f.put("Account", acc(3), map[string]model.Value{"owner": c1, "balance": decimal("-20")})

	uuid := func(n byte) string {
		return "00000000-0000-0000-0000-0000000000" + string("0123456789abcdef"[n>>4]) + string("0123456789abcdef"[n&15])
	}
	out, err := f.query("Account", "Details", f.actor("Client", c1), acc(1))
	f.expect(out, err, "AccountRow(accountId: "+uuid(1)+", balance: 100.5, frozen: false)")
	out, err = f.query("Account", "Details", f.actor("Client", c2), acc(1))
	f.expect(out, err, "error accountNotFound["+uuid(1)+"]")
	out, err = f.query("Account", "Details", f.actor("Client", c1), acc(9))
	f.expect(out, err, "error accountNotFound["+uuid(9)+"]")
	out, err = f.query("Account", "Details", role("compliance"), acc(1))
	f.expect(out, err, "error forbidden")

	out, err = f.query("Account", "MyAccounts", f.actor("Client", c1), pageReq(a, 1, ""))
	f.expect(out, err, "[[AccountRow(accountId: "+uuid(1)+", balance: 100.5, frozen: false)], 1]")
	out, err = f.query("Account", "MyAccounts", f.actor("Client", c1), pageReq(a, 1, "1"))
	f.expect(out, err, "[[AccountRow(accountId: "+uuid(3)+", balance: -20, frozen: false)], none]")
	out, err = f.query("Account", "MyAccounts", f.actor("Client", c1), pageReq(a, 0, ""))
	f.expect(out, err, "error invalidArgument[pageReq.size, range]")
	out, err = f.query("Account", "MyAccounts", f.actor("Client", c1))
	f.expect(out, err, "[[AccountRow(accountId: "+uuid(1)+", balance: 100.5, frozen: false), AccountRow(accountId: "+uuid(3)+", balance: -20, frozen: false)], none]")
	out, err = f.query("Account", "Details", f.actor("Client", c1), model.Int(5))
	f.expect(out, err, "error invalidArgument[accountId, type]")
	out, err = f.query("Account", "Details", f.actor("Client", c1))
	f.expect(out, err, "error invalidArgument[accountId, required]")

	f.env.fail = errors.New("database unavailable")
	if _, err := f.query("Account", "Details", f.actor("Client", c1), acc(1)); err == nil || !strings.Contains(err.Error(), "database unavailable") {
		t.Errorf("Env failure: %v", err)
	}
}

func TestTaxiQueries(t *testing.T) {
	for _, ext := range []string{"", "acme"} {
		t.Run("ext="+ext, func(t *testing.T) {
			f := load(t, example("taxi"), ext)
			a := f.env.a
			c1, d1, o1, o2 := f.id("Customer", 1), f.id("Driver", 1), f.id("Order", 1), f.id("Order", 2)
			car := f.value(a, "Car", map[string]model.Value{
				"plate": a.String("A001AA77"), "model": a.String("Kia Rio"), "carClass": f.enum("Dispatch.CarClass", "economy"),
			})
			f.put("Customer", c1, map[string]model.Value{"phone": a.String("+79990000000"), "activeOrder": o1})
			f.put("Driver", d1, map[string]model.Value{"name": a.String("Ivan"), "car": car,
				"status": f.enum("DriverStatus", "busy"), "currentTrip": f.id("Trip", 1)})
			f.put("Driver", f.id("Driver", 2), map[string]model.Value{"name": a.String("Peter"), "car": car,
				"status": f.enum("DriverStatus", "free")})
			price := f.value(a, "Ordering.Money", map[string]model.Value{"amount": decimal("350"), "currency": f.enum("Ordering.Currency", "rub")})
			at := model.Timestamp(f.now)
			f.put("Order", o1, map[string]model.Value{"customer": c1, "status": f.enum("OrderStatus", "assigned"),
				"price": price, "driverName": a.String("Ivan"), "driverPlate": a.String("A001AA77"), "placedAt": at})
			f.put("Order", o2, map[string]model.Value{"customer": c1, "status": f.enum("OrderStatus", "paid"),
				"price": price, "placedAt": model.Timestamp(f.now.Add(-time.Hour))})
			mart := func(o model.Value) model.Value { o.Type = f.typ("OrderMart"); return o }
			f.put("OrderMart", mart(o1), map[string]model.Value{"status": f.enum("OrderStatus", "assigned"),
				"driverName": a.String("Ivan"), "plate": a.String("A001AA77"), "placedAt": at})

			const u1, u2 = "00000000-0000-0000-0000-000000000001", "00000000-0000-0000-0000-000000000002"
			out, err := f.query("Customer", "Profile", f.actor("Customer", c1))
			f.expect(out, err, "CustomerProfile(phone: +79990000000, blocked: false)")
			out, err = f.query("Customer", "Profile", f.actor("Customer", f.id("Customer", 7)))
			f.expect(out, err, "error notRegistered")
			out, err = f.query("Order", "Card", f.actor("Customer", c1), o1)
			f.expect(out, err, "OrderCard(status: assigned, price: Money(amount: 350, currency: rub), driver: AssignedDriver(name: Ivan, plate: A001AA77), tip: none)")
			out, err = f.query("Order", "Card", f.actor("Customer", f.id("Customer", 2)), o1)
			f.expect(out, err, "error orderNotFound")
			out, err = f.query("Order", "MyOrders", f.actor("Customer", c1), pageReq(a, 20, ""))
			f.expect(out, err, "[[OrderRow(orderId: "+u1+", status: assigned, price: Money(amount: 350, currency: rub), placedAt: 2026-10-04T12:00:00Z), "+
				"OrderRow(orderId: "+u2+", status: paid, price: Money(amount: 350, currency: rub), placedAt: 2026-10-04T11:00:00Z)], none]")
			out, err = f.query("Driver", "MyState", f.actor("Driver", d1))
			f.expect(out, err, "DriverState(status: busy, currentTrip: "+u1+")")
			out, err = f.query("Driver", "FreeDrivers", role("dispatcher"), f.enum("Dispatch.CarClass", "economy"), pageReq(a, 5, ""))
			f.expect(out, err, "[[DriverInfo(driverId: "+u2+", name: Peter, plate: A001AA77)], none]")
			out, err = f.query("Driver", "FreeDrivers", f.actor("Driver", d1), f.enum("Dispatch.CarClass", "economy"), pageReq(a, 5, ""))
			f.expect(out, err, "error forbidden")
			out, err = f.query("OrderMart", "Board", role("dispatcher"), f.enum("OrderStatus", "assigned"), pageReq(a, 5, ""))
			f.expect(out, err, "[[BoardRow(orderId: "+u1+", status: assigned, driverName: Ivan, plate: A001AA77)], none]")

			rate, err := f.call(bytecode.FuncRule, "Ordering.Order.perKmRate", model.None, f.enum("Ordering.CarClass", "comfort"))
			want := map[string]string{"": "45", "acme": "25"}[ext]
			if err != nil || rate != want {
				t.Errorf("perKmRate(comfort) = %s, %v; expected %s", rate, err, want)
			}
		})
	}
}

func TestConstructs(t *testing.T) {
	f := load(t, "../../lower/testdata/constructs", "")
	a := f.env.a
	point := func(x, y int64) model.Value {
		return f.value(a, "Point", map[string]model.Value{"x": model.Int(x), "y": model.Int(y)})
	}
	for _, tc := range []struct {
		kind bytecode.FuncKind
		name string
		self model.Value
		args []model.Value
		want string
	}{
		{bytecode.FuncMethod, "Point.sum", point(2, 3), nil, "5"},
		{bytecode.FuncMethod, "Point.shifted", point(2, 3), []model.Value{model.Int(4)}, "Point(x: 6, y: 3)"},
		{bytecode.FuncMethod, "Point.total", point(2, 3), []model.Value{model.Int(1)}, "6"},
		{bytecode.FuncCheck, "Point.check", point(0, 3), nil, "true"},
		{bytecode.FuncCheck, "Point.check", point(-1, 3), nil, "false"},
		{bytecode.FuncRule, "Box.capacity", model.None, []model.Value{f.enum("Kind", "big")}, "20"},
		{bytecode.FuncRule, "Box.capacity", model.None, []model.Value{f.enum("Kind", "small")}, "-3"},
	} {
		got, err := f.call(tc.kind, tc.name, tc.self, tc.args...)
		if err != nil || got != tc.want {
			t.Errorf("%s = %s, %v; expected %s", tc.name, got, err, tc.want)
		}
	}
	// check inside logic: Point(x: -5) is a runtime failure.
	if _, err := f.call(bytecode.FuncMethod, "Point.shifted", point(2, 3), model.Int(-10)); !errors.Is(err, ErrCheck) {
		t.Errorf("shifted(-10): %v, expected a check failure", err)
	}
	// int overflow.
	if _, err := f.call(bytecode.FuncMethod, "Point.sum", point(1<<31-1, 1)); !errors.Is(err, ErrOverflow) {
		t.Errorf("sum with an overflow: %v", err)
	}

	box := func(n byte) model.Value { return f.id("BoxView", n) }
	k := keeper(f, 1)
	f.put("BoxView", box(1), map[string]model.Value{"kind": f.enum("Kind", "small"), "size": model.Int(5), "owner": k})
	f.put("BoxView", box(2), map[string]model.Value{"kind": f.enum("Kind", "small"), "size": model.Int(9)})
	f.put("BoxView", box(3), map[string]model.Value{"kind": f.enum("Kind", "big"), "size": model.Int(30)})
	const u = "00000000-0000-0000-0000-00000000000"
	out, err := f.query("BoxView", "Small", role("admin"), model.Int(5))
	f.expect(out, err, "[BoxRow(boxId: "+u+"2, size: 9), BoxRow(boxId: "+u+"1, size: 5)]")
	out, err = f.query("BoxView", "Total", role("admin"), f.enum("Kind", "small"))
	f.expect(out, err, "BoxStats(total: 14)")
	out, err = f.query("BoxView", "Orphans", role("admin"))
	f.expect(out, err, "[BoxRow(boxId: "+u+"2, size: 9), BoxRow(boxId: "+u+"3, size: 30)]")
	out, err = f.query("Box", "Stats", role("admin"), f.enum("Kind", "big"))
	f.expect(out, err, "BoxStats(total: 0)")
	out, err = f.query("BoxView", "Small", role("admin"), model.Int(1<<40))
	f.expect(out, err, "error invalidArgument[top, range]")
}

func keeper(f *fixture, n byte) model.Value { return f.id("Keeper", n) }

func TestLimits(t *testing.T) {
	f := load(t, example("bank"), "")
	f.m.limits.Iterations = 1
	c1 := f.id("Client", 1)
	for i := byte(1); i <= 3; i++ {
		f.put("Account", f.id("Account", i), map[string]model.Value{"owner": c1})
	}
	_, err := f.query("Account", "MyAccounts", f.actor("Client", c1), pageReq(f.env.a, 5, ""))
	var flt *Fault
	if !errors.As(err, &flt) || !errors.Is(err, ErrLimit) || flt.Func != "Accounts.Account.MyAccounts" {
		t.Errorf("iteration limit: %v", err)
	}
	f.m.limits = DefaultLimits
	f.m.limits.Depth = 0
	if _, err := f.query("Account", "Details", f.actor("Client", c1), f.id("Account", 1)); !errors.Is(err, ErrLimit) {
		t.Errorf("depth limit: %v", err)
	}
}

// TestConcurrent: one machine serves queries from many goroutines.
func TestConcurrent(t *testing.T) {
	f := load(t, example("bank"), "")
	c1 := f.id("Client", 1)
	f.put("Account", f.id("Account", 1), map[string]model.Value{"owner": c1, "balance": decimal("5")})
	call := model.QueryCall{Type: f.typ("Account"), Query: f.s.Lookup("Account").Method("Details").Ref,
		Args: f.env.a.Record(0, f.id("Account", 1)), Arena: f.env.a, Caller: f.actor("Client", c1), Now: f.now}
	done := make(chan string)
	for range 8 {
		go func() {
			var last string
			for range 200 {
				out, err := f.m.Query(context.Background(), call, f.env)
				if err != nil {
					last = err.Error()
					break
				}
				last = f.show(out)
				out.Release()
			}
			done <- last
		}()
	}
	for range 8 {
		if got := <-done; !strings.HasSuffix(got, "balance: 5, frozen: false)") {
			t.Errorf("result: %s", got)
		}
	}
}
