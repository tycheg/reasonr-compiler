package dec

import (
	"math"
	"math/big"
	"math/rand"
	"testing"
)

var (
	bigScale = new(big.Int).Exp(big.NewInt(10), big.NewInt(Scale), nil)
	bigLimit = new(big.Int).Exp(big.NewInt(10), big.NewInt(Digits), nil)
	maxDec   = "99999999999999999999.999999999999999999"
)

// toBig returns the 128-bit representation of a number as a big.Int.
func toBig(d Dec) *big.Int {
	b := new(big.Int).SetUint64(d.hi)
	b.Lsh(b, 64)
	b.Or(b, new(big.Int).SetUint64(d.lo))
	if d.negative() {
		b.Sub(b, new(big.Int).Lsh(big.NewInt(1), 128))
	}
	return b
}

// fromBig is the inverse of toBig; ok == false means out of range.
func fromBig(b *big.Int) (Dec, bool) {
	if new(big.Int).Abs(b).Cmp(bigLimit) >= 0 {
		return Dec{}, false
	}
	m := new(big.Int).Abs(b)
	lo := new(big.Int).And(m, new(big.Int).SetUint64(math.MaxUint64)).Uint64()
	hi := new(big.Int).Rsh(m, 64).Uint64()
	return make128(b.Sign() < 0, u128{hi, lo}), true
}

// divRound divides n by d with rounding: even to the nearest even, otherwise away from zero.
func divRound(n, d *big.Int, even bool) *big.Int {
	q, r := new(big.Int).QuoRem(n, d, new(big.Int))
	if r.Sign() == 0 {
		return q
	}
	twice := new(big.Int).Abs(r)
	twice.Lsh(twice, 1)
	c := twice.Cmp(new(big.Int).Abs(d))
	if c > 0 || c == 0 && (!even || q.Bit(0) == 1) {
		if n.Sign()*d.Sign() < 0 {
			q.Sub(q, big.NewInt(1))
		} else {
			q.Add(q, big.NewInt(1))
		}
	}
	return q
}

func TestParseString(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"0", "0"},
		{"-0", "0"},
		{"+1", "1"},
		{"1.50", "1.5"},
		{"-1.5", "-1.5"},
		{"007.010", "7.01"},
		{"0.000000000000000001", "0.000000000000000001"},
		{"0.0000000000000000005", "0"}, // a half goes to even (0)
		{"0.0000000000000000015", "0.000000000000000002"},
		{"0.00000000000000000050001", "0.000000000000000001"},
		{"-0.0000000000000000025", "-0.000000000000000002"},
		{maxDec, maxDec},
		{"-" + maxDec, "-" + maxDec},
		{"12345678901234567890", "12345678901234567890"},
	} {
		d, err := Parse(tc.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.in, err)
			continue
		}
		if got := d.String(); got != tc.want {
			t.Errorf("Parse(%q) = %s, want %s", tc.in, got, tc.want)
		}
	}
	for _, in := range []string{"", "-", ".5", "5.", "1..2", "1e3", "1,5", "abc", "1.2.3",
		"100000000000000000000", maxDec + "9", "99999999999999999999.9999999999999999995"} {
		if d, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) = %s, want error", in, d)
		}
	}
}

func TestArith(t *testing.T) {
	type op func(a, b Dec) (Dec, bool)
	ops := map[string]op{"+": Dec.Add, "-": Dec.Sub, "*": Dec.Mul, "/": Dec.Quo}
	for _, tc := range []struct {
		a, op, b, want string // want "" means an overflow or division by zero
	}{
		{"1.1", "+", "2.2", "3.3"},
		{"-1.1", "+", "1.1", "0"},
		{"0.1", "-", "0.3", "-0.2"},
		{maxDec, "+", "0.000000000000000001", ""},
		{"-" + maxDec, "-", "0.000000000000000001", ""},
		{"1.5", "*", "-2", "-3"},
		{"0.000000000000000001", "*", "0.5", "0"},                      // 0.5e-18 → 0 (even)
		{"0.000000000000000003", "*", "0.5", "0.000000000000000002"},   // 1.5e-18 → 2e-18
		{"-0.000000000000000003", "*", "0.5", "-0.000000000000000002"}, // symmetric
		{"10000000000", "*", "10000000000", ""},
		{"1", "/", "3", "0.333333333333333333"},
		{"2", "/", "3", "0.666666666666666667"},
		{"-2", "/", "3", "-0.666666666666666667"},
		{"1", "/", "0", ""},
		{"0", "/", "0", ""},
		{"1", "/", "0.000000000000000001", "1000000000000000000"},
		{"100", "/", "0.000000000000000001", ""},
		{maxDec, "/", maxDec, "1"},
		{"1", "/", maxDec, "0"},
	} {
		got, ok := ops[tc.op](MustParse(tc.a), MustParse(tc.b))
		switch {
		case tc.want == "" && ok:
			t.Errorf("%s %s %s = %s, want overflow", tc.a, tc.op, tc.b, got)
		case tc.want != "" && !ok:
			t.Errorf("%s %s %s: overflow, want %s", tc.a, tc.op, tc.b, tc.want)
		case ok && got.String() != tc.want:
			t.Errorf("%s %s %s = %s, want %s", tc.a, tc.op, tc.b, got, tc.want)
		}
	}
}

func TestRound(t *testing.T) {
	for _, tc := range []struct {
		x    string
		n    int
		want string
	}{
		{"2.345", 2, "2.35"},
		{"-2.345", 2, "-2.35"},
		{"2.344", 2, "2.34"},
		{"2.5", 0, "3"},
		{"-2.5", 0, "-3"},
		{"0.5", 0, "1"},
		{"1234.5", -2, "1200"},
		{"1250", -2, "1300"},
		{"7", 30, "7"},
		{"0.4", 0, "0"},
		{"-0.4", 0, "0"},
		{"49999999999999999999", -20, "0"},
		{"50000000000000000000", -20, ""}, // 10²⁰ is out of range
		{maxDec, 0, ""},
		{maxDec, -40, "0"},
	} {
		got, ok := MustParse(tc.x).Round(tc.n)
		switch {
		case tc.want == "" && ok:
			t.Errorf("round(%s, %d) = %s, want overflow", tc.x, tc.n, got)
		case tc.want != "" && !ok:
			t.Errorf("round(%s, %d): overflow, want %s", tc.x, tc.n, tc.want)
		case ok && got.String() != tc.want:
			t.Errorf("round(%s, %d) = %s, want %s", tc.x, tc.n, got, tc.want)
		}
	}
	for _, tc := range []struct{ x, floor, ceil, trunc string }{
		{"2.5", "2", "3", "2"},
		{"-2.5", "-3", "-2", "-2"},
		{"3", "3", "3", "3"},
		{"-0.1", "-1", "0", "0"},
	} {
		x := MustParse(tc.x)
		f, _ := x.Floor()
		c, _ := x.Ceil()
		if f.String() != tc.floor || c.String() != tc.ceil || x.Trunc().String() != tc.trunc {
			t.Errorf("%s: floor %s ceil %s trunc %s, want %s %s %s", tc.x, f, c, x.Trunc(), tc.floor, tc.ceil, tc.trunc)
		}
	}
}

func TestConvert(t *testing.T) {
	for _, x := range []int64{0, 1, -1, 42, math.MaxInt64, math.MinInt64} {
		d := FromInt(x)
		if got, ok := d.Int64(); !ok || got != x {
			t.Errorf("FromInt(%d).Int64() = %d, %v", x, got, ok)
		}
		if !d.IsInt() {
			t.Errorf("FromInt(%d) is not an integer", x)
		}
	}
	if _, ok := MustParse("9223372036854775808").Int64(); ok {
		t.Error("2^63 fit into int64")
	}
	if got, ok := MustParse("-9.99").Int64(); !ok || got != -9 {
		t.Errorf("Int64(-9.99) = %d, %v", got, ok)
	}
	for _, tc := range []struct {
		f    float64
		want string
	}{{0.1, "0.1"}, {-2.5, "-2.5"}, {1e-19, "0"}, {123456.789, "123456.789"}, {1e19, "10000000000000000000"}} {
		d, ok := FromFloat(tc.f)
		if !ok || d.String() != tc.want {
			t.Errorf("FromFloat(%v) = %s, %v, want %s", tc.f, d, ok, tc.want)
		}
		if got := d.Float64(); got != tc.f && tc.f != 1e-19 {
			t.Errorf("Float64(%s) = %v, want %v", d, got, tc.f)
		}
	}
	for _, f := range []float64{math.NaN(), math.Inf(1), 1e20, -1e25} {
		if _, ok := FromFloat(f); ok {
			t.Errorf("FromFloat(%v) ok", f)
		}
	}
	if _, ok := FromBits(limit.lo, limit.hi); ok {
		t.Error("FromBits(10^38) ok")
	}
}

func TestCmpSign(t *testing.T) {
	vals := []string{"-" + maxDec, "-1", "-0.000000000000000001", "0", "0.000000000000000001", "1", maxDec}
	for i, a := range vals {
		for j, b := range vals {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := MustParse(a).Cmp(MustParse(b)); got != want {
				t.Errorf("Cmp(%s, %s) = %d, want %d", a, b, got, want)
			}
		}
	}
	if MustParse("-3").Sign() != -1 || MustParse("0").Sign() != 0 || MustParse("3").Sign() != 1 {
		t.Error("Sign")
	}
	if MustParse("-3").Abs().String() != "3" || MustParse("3").Neg().String() != "-3" {
		t.Error("Abs/Neg")
	}
}

// randDec returns a random number: different magnitudes to hit both
// overflows and roundings.
func randDec(r *rand.Rand) Dec {
	digits := r.Intn(Digits) + 1
	m := new(big.Int).Rand(r, new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits)), nil))
	if r.Intn(2) == 0 {
		m.Neg(m)
	}
	d, _ := fromBig(m)
	return d
}

func TestAgainstBig(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for range 200000 {
		checkBig(t, randDec(r), randDec(r), r.Intn(45)-22)
	}
}

func FuzzArith(f *testing.F) {
	f.Add(uint64(1), uint64(0), uint64(3), uint64(0), 2)
	f.Add(limit.lo-1, limit.hi, uint64(7), uint64(0), -3)
	f.Fuzz(func(t *testing.T, alo, ahi, blo, bhi uint64, n int) {
		a, ok1 := FromBits(alo, ahi)
		b, ok2 := FromBits(blo, bhi)
		if !ok1 || !ok2 {
			return
		}
		checkBig(t, a, b, n%50)
	})
}

// checkBig compares operations on a and b with computations in math/big.
func checkBig(t *testing.T, a, b Dec, n int) {
	t.Helper()
	A, B := toBig(a), toBig(b)
	check := func(op string, got Dec, ok bool, want *big.Int) {
		t.Helper()
		w, wok := fromBig(want)
		if ok != wok || ok && got != w {
			t.Fatalf("%s %s %s = %s (%v), want %s (%v)", a, op, b, got, ok, w, wok)
		}
	}
	got, ok := a.Add(b)
	check("+", got, ok, new(big.Int).Add(A, B))
	got, ok = a.Sub(b)
	check("-", got, ok, new(big.Int).Sub(A, B))
	got, ok = a.Mul(b)
	check("*", got, ok, divRound(new(big.Int).Mul(A, B), bigScale, true))
	got, ok = a.Quo(b)
	if B.Sign() == 0 {
		if ok {
			t.Fatalf("%s / 0 ok", a)
		}
	} else {
		check("/", got, ok, divRound(new(big.Int).Mul(A, bigScale), B, true))
	}
	got, ok = a.Rem(b)
	if B.Sign() == 0 {
		if ok {
			t.Fatalf("%s %% 0 ok", a)
		}
	} else {
		check("%", got, ok, new(big.Int).Rem(A, B))
	}
	got, ok = a.Round(n)
	if k := Scale - n; k <= 0 {
		check("round", got, ok, A)
	} else {
		p := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(k)), nil)
		check("round", got, ok, new(big.Int).Mul(divRound(A, p, false), p))
	}
	if c := a.Cmp(b); c != A.Cmp(B) {
		t.Fatalf("Cmp(%s, %s) = %d", a, b, c)
	}
	s := a.String()
	if back, err := Parse(s); err != nil || back != a {
		t.Fatalf("Parse(String(%s)) = %s, %v", s, back, err)
	}
}

func BenchmarkMul(b *testing.B) {
	x, y := MustParse("12345.678"), MustParse("0.9876")
	for range b.N {
		x.Mul(y)
	}
}

func BenchmarkQuo(b *testing.B) {
	x, y := MustParse("12345.678"), MustParse("3")
	for range b.N {
		x.Quo(y)
	}
}

func BenchmarkAppend(b *testing.B) {
	x := MustParse("-12345.678")
	var buf [48]byte
	for range b.N {
		x.Append(buf[:0])
	}
}
