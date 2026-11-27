package tools_test

import (
	"testing"
	"testing/fstest"

	"github.com/tycheg/reasonr-compiler/internal/pipeline"
	"github.com/tycheg/reasonr-compiler/internal/source"
	"github.com/tycheg/reasonr-compiler/internal/tools"
)

const flagged = `role admin

## Tips
feature tips "2026-11-07"
feature bonus "2026-11-08"

aggregate Order
  id uuid
  tip int
  note string

  error tipsDisabled()

  command(admin) LeaveTip(amount int, big bool)
    if tips
      tipped(amount: amount)
    else
      tipsDisabled()
    if big and tips
      tipped(amount: amount * 2)
    if amount > 100
      noted(text: "much")
    else if tips
      noted(text: "tips")
    else
      noted(text: "little")
    if not tips
      return
    noted(text: "end")

  command(admin) Only()
    if tips
      noted(text: "only")

  command(admin) Bonus()
    if bonus
      noted(text: "bonus")
    closed(withTips: tips)

  event tipped(amount int)
    tip = amount

  event noted(text string)
    note = text

  event closed(withTips bool)
    note = "closed"

screen Tips "Tips"
  Form [new Order.Id].LeaveTip if tips
  Form [new Order.Id].Only if tips
  Form [new Order.Id].Bonus

app(admin) Admin "Admin"
  Menu
    Tips

test "Tips"
  feature tips, bonus
  let o1 = new Order.Id
  when
    [o1].LeaveTip(amount: 10, big: false) as admin

test "Only tips"
  feature tips
  let o1 = new Order.Id
  when
    [o1].Only() as admin

test "No flags"
  let o1 = new Order.Id
  when
    [o1].LeaveTip(amount: 10, big: false) as admin
  then
    error tipsDisabled
`

const removedOn = `role admin

feature bonus "2026-11-08"

aggregate Order
  id uuid
  tip int
  note string

  error tipsDisabled()

  command(admin) LeaveTip(amount int, big bool)
    tipped(amount: amount)
    if big
      tipped(amount: amount * 2)
    if amount > 100
      noted(text: "much")
    else
      noted(text: "tips")
    noted(text: "end")

  command(admin) Only()
    noted(text: "only")

  command(admin) Bonus()
    if bonus
      noted(text: "bonus")
    closed(withTips: true)

  event tipped(amount int)
    tip = amount

  event noted(text string)
    note = text

  event closed(withTips bool)
    note = "closed"

screen Tips "Tips"
  Form [new Order.Id].LeaveTip
  Form [new Order.Id].Only
  Form [new Order.Id].Bonus

app(admin) Admin "Admin"
  Menu
    Tips

test "Tips"
  feature bonus
  let o1 = new Order.Id
  when
    [o1].LeaveTip(amount: 10, big: false) as admin

test "Only tips"
  let o1 = new Order.Id
  when
    [o1].Only() as admin

test "No flags"
  let o1 = new Order.Id
  when
    [o1].LeaveTip(amount: 10, big: false) as admin
  then
    error tipsDisabled
`

const removedOff = `role admin

feature bonus "2026-11-08"

aggregate Order
  id uuid
  tip int
  note string

  error tipsDisabled()

  command(admin) LeaveTip(amount int, big bool)
    tipsDisabled()
    if amount > 100
      noted(text: "much")
    else
      noted(text: "little")
    return
    noted(text: "end")

  command(admin) Only()
    return

  command(admin) Bonus()
    if bonus
      noted(text: "bonus")
    closed(withTips: false)

  event tipped(amount int)
    tip = amount

  event noted(text string)
    note = text

  event closed(withTips bool)
    note = "closed"

screen Tips "Tips"
  Form [new Order.Id].Bonus

app(admin) Admin "Admin"
  Menu
    Tips

test "No flags"
  let o1 = new Order.Id
  when
    [o1].LeaveTip(amount: 10, big: false) as admin
  then
    error tipsDisabled
`

func TestRemoveFeature(t *testing.T) {
	for _, c := range []struct {
		name  string
		off   bool
		want  string
		tests int
	}{{"on", false, removedOn, 0}, {"off", true, removedOff, 2}} {
		t.Run(c.name, func(t *testing.T) {
			mod, err := pipeline.OpenFS(source.NewFileSet(), fstest.MapFS{"m.rr": {Data: []byte(flagged)}}, "m")
			if err != nil {
				t.Fatal(err)
			}
			m, err := mod.Model("")
			if err != nil || m.HasErrors() {
				t.Fatalf("model: %v %v", err, m.Diagnostics)
			}
			r, err := tools.RemoveFeature([]*pipeline.Model{m}, "tips", c.off)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(r.Files["m/m.rr"]); got != c.want {
				t.Errorf("got:\n%s", got)
			}
			if len(r.Tests) != c.tests || r.Feature != "tips" {
				t.Errorf("flag %s, removed tests %v", r.Feature, r.Tests)
			}
		})
	}
}
