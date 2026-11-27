package lsp

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tycheg/reasonr-compiler/internal/diag"
)

// entityTemplate is an entity of about 50 lines; N is replaced with a number.
const entityTemplate = `## Order N
aggregate OrderN
  id uuid
  customer CustomerN.Id?
  status StatusN
  total decimal
  note string?

  index byCustomerN(customer)

  ## Cancel the order
  command(customer) Cancel(reason string)
    if status == StatusN.cancelled
      alreadyCancelled()
    if reason.length > 100
      reasonTooLong(max: 100)
    if hasErrors
      return
    cancelled(why: reason.trim())

  command(customer) Pay(sum decimal)
    if sum <= 0
      invalidSum(sum: sum)
      return
    paid(sum: sum)
    [customer]?.charge(sum: sum)

  event cancelled(why string)
    status = StatusN.cancelled
    note = why

  event paid(sum decimal)
    total = total + sum
    status = StatusN.paid

  error alreadyCancelled
  error reasonTooLong(max int)
  error invalidSum(sum decimal)

  query(CustomerN) MyOrders(pageReq PageRequest) page<RowN>
    return [OrderN]
      where customer == me
      page pageReq
      select RowN(orderId: id, total: total)

enum StatusN
  placed
  paid
  cancelled

value RowN
  orderId OrderN.Id
  total decimal

actor CustomerN
  id uuid
  phone string? personal
  spent decimal

  command charge(sum decimal)
    charged(sum: sum)

  event charged(sum decimal)
    spent = spent + sum

`

// writeLargeModule creates a module of files files with perFile entities
// each and returns the directory and the number of lines.
func writeLargeModule(t testing.TB, files, perFile int) (string, int) {
	dir := t.TempDir()
	dir, _ = filepath.EvalSymlinks(dir)
	lines := 0
	for f := 0; f < files; f++ {
		var b strings.Builder
		if f == 0 {
			b.WriteString("role admin\n\n")
		}
		for e := 0; e < perFile; e++ {
			b.WriteString(strings.ReplaceAll(entityTemplate, "N", fmt.Sprint(f*perFile+e)))
		}
		text := strings.TrimRight(b.String(), "\n") + "\n"
		lines += strings.Count(text, "\n")
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("m%02d.rr", f)), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, lines
}

// BenchmarkEdit edits one file in a 10k-line module: a load with the parse
// cache and a full model check.
func BenchmarkEdit(b *testing.B) {
	dir, lines := writeLargeModule(b, 10, 16)
	ws := newWorkspace()
	path := filepath.Join(dir, "m03.rr")
	text, _ := os.ReadFile(path)
	ws.docs[path] = &document{path: path, text: text}
	m := ws.module(path)
	snap, err := ws.analyze(m, nil, "")
	if err != nil {
		b.Fatal(err)
	}
	for _, d := range snap.base.Diagnostics {
		if d.Severity() == diag.Error {
			b.Fatalf("%d lines: %s: %s", lines, d.Code(), d.Message())
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ws.docs[path].text = append(append([]byte(nil), text...), fmt.Sprintf("# edit %d\n", i)...)
		if _, err := ws.analyze(m, nil, ""); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportMetric(float64(lines), "lines")
}
