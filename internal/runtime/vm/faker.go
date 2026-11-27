package vm

import (
	"errors"
	"fmt"
	"hash/fnv"
	"math/rand/v2"
	"strings"

	"github.com/tycheg/reasonr-compiler/internal/model"
	"github.com/tycheg/reasonr-compiler/internal/runtime/dec"
)

// range and faker are built-in functions of tests and demo scenarios
// (RFC §27.5). faker is deterministic: a test generator
// sets the run seed and the test name; phone and email do not repeat
// within a test run.

// TestOptions are the settings of a test run.
type TestOptions struct {
	// Seed is the faker seed; runs with the same Seed give the same values. 0
	// seeds from the test name only.
	Seed uint64
	// Phase is the part of the test: all of it, only given or only the when
	// path (performance tests, RFC §27.6).
	Phase Phase
}

// Phase is the part of a test a run executes.
type Phase uint8

const (
	PhaseAll   Phase = iota
	PhaseGiven       // given, before when: filling data before the load
	PhaseWhen        // the given steps are skipped: the path of one virtual user
)

// faker is the faker state of a test run.
type faker struct {
	rnd  *rand.Rand
	seen map[string]bool
}

func newFaker(name string, o TestOptions) *faker {
	h := fnv.New64a()
	h.Write([]byte(name))
	return &faker{rnd: rand.New(rand.NewPCG(h.Sum64(), o.Seed)), seen: map[string]bool{}}
}

var errNoTest = errors.New("faker is only for tests and demo scenarios")

func scenario(fns map[string]builtin) {
	fns["range"] = func(r *run, x []model.Value) (model.Value, error) {
		n := x[0].Int()
		if n < 0 || n > int64(r.m.limits.Iterations) {
			return model.None, fmt.Errorf("%w: range(%d): from 0 to %d", ErrLimit, n, r.m.limits.Iterations)
		}
		items := make([]model.Value, n)
		for i := range items {
			items[i] = model.Int(int64(i))
		}
		return r.a.List(items...), nil
	}
	fns["perf.stat"] = func(r *run, x []model.Value) (model.Value, error) {
		if r.test == nil || r.test.stat == nil {
			return model.None, fmt.Errorf("%w: stats is only for performance test checks", ErrUnsupported)
		}
		return r.test.stat(r.a.Text(x[0]))
	}
	str := func(f func(*faker) string) builtin {
		return func(r *run, _ []model.Value) (model.Value, error) {
			if r.test == nil {
				return model.None, errNoTest
			}
			return r.a.String(f(r.test.faker)), nil
		}
	}
	fns["faker.firstName"] = str(func(f *faker) string { return f.firstName() })
	fns["faker.lastName"] = str(func(f *faker) string { return f.lastName() })
	fns["faker.name"] = str(func(f *faker) string { return f.firstName() + " " + f.lastName() })
	fns["faker.phone"] = str(func(f *faker) string {
		return f.unique(func() string { return f.pattern("+7 9## ###-##-##") })
	})
	fns["faker.email"] = str(func(f *faker) string {
		return f.unique(func() string {
			return fmt.Sprintf("%s.%s%d@example.com", f.pick(firstEN), strings.ToLower(f.pick(lastEN)), f.rnd.IntN(1000))
		})
	})
	fns["faker.address"] = str(func(f *faker) string {
		return fmt.Sprintf("%d %s St", 1+f.rnd.IntN(300), f.pick(lastEN))
	})
	fns["faker.company"] = str(func(f *faker) string {
		return f.pick(lastEN) + " " + f.pick([]string{"Inc", "LLC", "Group", "Partners"})
	})
	fns["faker.text"] = func(r *run, x []model.Value) (model.Value, error) {
		if r.test == nil {
			return model.None, errNoTest
		}
		f := r.test.faker
		n := max(x[0].Int(), 0)
		out := make([]string, 0, n)
		for range n {
			out = append(out, f.pick(wordsEN))
		}
		return r.a.String(strings.Join(out, " ")), nil
	}
	fns["faker.int"] = func(r *run, x []model.Value) (model.Value, error) {
		if r.test == nil {
			return model.None, errNoTest
		}
		a, b := x[0].Int(), x[1].Int()
		if b < a {
			a, b = b, a
		}
		return model.Int(a + r.test.faker.rnd.Int64N(b-a+1)), nil
	}
	fns["faker.float"] = func(r *run, x []model.Value) (model.Value, error) {
		if r.test == nil {
			return model.None, errNoTest
		}
		a, b := x[0].Float(), x[1].Float()
		return model.Float(a + (b-a)*r.test.faker.rnd.Float64()), nil
	}
	fns["faker.decimal"] = func(r *run, x []model.Value) (model.Value, error) {
		if r.test == nil {
			return model.None, errNoTest
		}
		a, b := x[0].Decimal().Float64(), x[1].Decimal().Float64()
		d, ok := dec.FromFloat(a + (b-a)*r.test.faker.rnd.Float64())
		if ok {
			d, ok = d.Round(2)
		}
		if !ok {
			return model.None, ErrOverflow
		}
		return model.Decimal(d), nil
	}
	fns["faker.chance"] = func(r *run, x []model.Value) (model.Value, error) {
		if r.test == nil {
			return model.None, errNoTest
		}
		return model.Bool(r.test.faker.rnd.Float64() < x[0].Decimal().Float64()), nil
	}
	fns["faker.pick"] = func(r *run, x []model.Value) (model.Value, error) {
		if r.test == nil {
			return model.None, errNoTest
		}
		items := r.a.Elems(x[0])
		if len(items) == 0 {
			return model.None, fmt.Errorf("%w: faker.pick: empty list", ErrUnsupported)
		}
		return items[r.test.faker.rnd.IntN(len(items))], nil
	}
	fns["faker.date"] = func(r *run, x []model.Value) (model.Value, error) {
		if r.test == nil {
			return model.None, errNoTest
		}
		a, b := x[0].Days(), x[1].Days()
		if b < a {
			a, b = b, a
		}
		return model.DateDays(a + r.test.faker.rnd.Int64N(b-a+1)), nil
	}
	fns["faker.pattern"] = func(r *run, x []model.Value) (model.Value, error) {
		if r.test == nil {
			return model.None, errNoTest
		}
		return r.a.String(r.test.faker.pattern(r.a.Text(x[0]))), nil
	}
}

func (f *faker) pick(xs []string) string { return xs[f.rnd.IntN(len(xs))] }

func (f *faker) firstName() string { return f.pick(firstEN) }

func (f *faker) lastName() string { return f.pick(lastEN) }

// pattern: # is a digit, ? is a capital letter.
func (f *faker) pattern(p string) string {
	const letters = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	var b strings.Builder
	for _, c := range p {
		switch c {
		case '#':
			b.WriteByte(byte('0' + f.rnd.IntN(10)))
		case '?':
			b.WriteByte(letters[f.rnd.IntN(len(letters))])
		default:
			b.WriteRune(c)
		}
	}
	return b.String()
}

// unique repeats gen until the value is new in the run.
func (f *faker) unique(gen func() string) string {
	for {
		if v := gen(); !f.seen[v] {
			f.seen[v] = true
			return v
		}
	}
}

var (
	firstEN = []string{"Alice", "Bob", "Carol", "David", "Emma", "Frank", "Grace", "Henry", "Ivy", "Jack", "Kate", "Liam"}
	lastEN  = []string{"Smith", "Johnson", "Brown", "Taylor", "Miller", "Wilson", "Moore", "Clark", "Lewis", "Walker"}
	wordsEN = []string{"quick", "order", "road", "city", "morning", "evening", "trip", "address", "car", "client", "time", "place"}
)
