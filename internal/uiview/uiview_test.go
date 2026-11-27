package uiview_test

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	reasonr "github.com/tycheg/reasonr-compiler"
	"github.com/tycheg/reasonr-compiler/internal/golden"
	"github.com/tycheg/reasonr-compiler/internal/uiview"
)

// TestTaxi: the taxi manifests and screen descriptions match the golden
// files; the description hash changes with the language.
func TestTaxi(t *testing.T) {
	m, err := reasonr.Build(filepath.Join("..", "..", "examples", "taxi", "src"), reasonr.Options{Test: true})
	if err != nil {
		t.Fatal(err)
	}
	ui := m.UI()
	b := uiview.New(ui, "Admin")
	for _, c := range []struct {
		name string
		v    any
	}{
		{"admin.manifest.json", b.Manifest("en", "web")},
		{"admin.order.json", b.Screen("AdminOrder", "en", "web")},
		{"admin.orders.json", b.Screen("AdminOrders", "en", "web")},
		{"admin.neworder.json", b.Screen("AdminNewOrder", "en", "web")},
		{"taxi.myorders.json", uiview.New(ui, "Taxi").Screen("MyOrders", "en", "web")},
	} {
		data, err := json.MarshalIndent(c.v, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		golden.Check(t, filepath.Join("testdata", c.name), append(data, '\n'))
	}
	// The description holds only what is on the screen.
	data, _ := json.Marshal(b.Screen("AdminOrder", "en", "web"))
	for _, other := range []string{"BoardRow", "DriverState", "Shift", "OrderMart", "MyOrders", "CustomerProfile"} {
		if strings.Contains(string(data), other) {
			t.Errorf("the AdminOrder description has %s", other)
		}
	}
	if b.Screen("Shift", "en", "web") != nil {
		t.Error("a screen of another app is described")
	}
	if uiview.Hash(b.Screen("AdminOrder", "en", "web")) == uiview.Hash(b.Screen("AdminOrder", "de", "web")) {
		t.Error("the hash does not depend on the language")
	}
}

func TestHumanize(t *testing.T) {
	for in, want := range map[string]string{"placedAt": "Placed at", "status": "Status", "OrderTaxi": "Order taxi"} {
		if got := uiview.Humanize(in); got != want {
			t.Errorf("Humanize(%q) = %q, want %q", in, got, want)
		}
	}
}
