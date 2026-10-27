package decimal

import (
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"math/rand"
	"testing"
)

func TestParseAndString(t *testing.T) {
	ok := []struct{ in, want string }{
		{"8.1", "8.1"}, {"0", "0"}, {"-0", "0"}, {"+7", "7"}, {".5", "0.5"}, {"5.", "5"},
		{"100.000000", "100"}, {"-0.05", "-0.05"}, {"0.000001", "0.000001"},
		{"1234.5000000000", "1234.5"}, // excess zeros are fine
		{"9223372036854.775807", "9223372036854.775807"},
		{"-9223372036854.775808", "-9223372036854.775808"},
	}
	for _, c := range ok {
		d, err := Parse(c.in)
		if err != nil || d.String() != c.want {
			t.Errorf("Parse(%q) = %v, %v; want %s", c.in, d, err, c.want)
		}
	}
	bad := []struct {
		in  string
		err error
	}{
		{"", ErrSyntax}, {".", ErrSyntax}, {"-", ErrSyntax}, {"1e3", ErrSyntax}, {"1,5", ErrSyntax},
		{" 1", ErrSyntax}, {"1 ", ErrSyntax}, {"1_0", ErrSyntax}, {"--1", ErrSyntax},
		{"0.0000001", ErrPrecision}, {"8.100000381469727", ErrPrecision},
		{"9223372036854.775808", ErrOverflow}, {"-9223372036854.775809", ErrOverflow},
		{"99999999999999999999999999", ErrOverflow},
	}
	for _, c := range bad {
		if _, err := Parse(c.in); !errors.Is(err, c.err) {
			t.Errorf("Parse(%q) error = %v; want %v", c.in, err, c.err)
		}
	}
}

func TestParseRound(t *testing.T) {
	cases := []struct {
		in   string
		mode RoundingMode
		want string
	}{
		{"0.0000005", RoundHalfUp, "0.000001"},
		{"0.0000005", RoundHalfEven, "0"},
		{"0.0000015", RoundHalfEven, "0.000002"},
		{"0.00000050001", RoundHalfEven, "0.000001"},
		{"-0.0000005", RoundHalfUp, "-0.000001"},
		{"0.0000009", RoundDown, "0"},
		{"-0.0000001", RoundFloor, "-0.000001"},
		{"0.0000001", RoundCeiling, "0.000001"},
		{"8.100000381469727", RoundHalfUp, "8.1"},
	}
	for _, c := range cases {
		d, err := ParseRound(c.in, c.mode)
		if err != nil || d.String() != c.want {
			t.Errorf("ParseRound(%q, %d) = %v, %v; want %s", c.in, c.mode, d, err, c.want)
		}
	}
}

func TestInvoiceExample(t *testing.T) {
	net := MustParse("1234.50")
	rate := MustParse("0.081")
	vat := net.Mul(rate).Round(2, RoundHalfUp)
	if vat.String() != "99.99" { // 1234.5 * 0.081 = 99.9945
		t.Errorf("vat = %s", vat)
	}
	total := net.Add(vat)
	if total.StringFixed(2) != "1334.49" {
		t.Errorf("total = %s", total.StringFixed(2))
	}
	// Swiss 5-Rappen rounding
	for in, want := range map[string]string{
		"10.02": "10", "10.03": "10.05", "10.025": "10.05", "10.074": "10.05",
		"10.075": "10.1", "-10.025": "-10.05", "-10.02": "-10",
	} {
		if got := MustParse(in).RoundTo(MustParse("0.05"), RoundHalfUp).String(); got != want {
			t.Errorf("RoundTo(0.05) of %s = %s; want %s", in, got, want)
		}
	}
	if got := MustParse("19.95").MulInt(3).String(); got != "59.85" {
		t.Errorf("MulInt = %s", got)
	}
	if got := MustParse("100").Div(MustParse("3")).String(); got != "33.333333" {
		t.Errorf("Div = %s", got)
	}
}

func TestStringFixed(t *testing.T) {
	cases := []struct {
		in     string
		places int
		want   string
	}{
		{"1", 2, "1.00"}, {"-0.001", 2, "0.00"}, {"2.675", 2, "2.68"}, {"-2.675", 2, "-2.68"},
		{"5", 0, "5"}, {"0.5", 0, "1"}, {"1.5", 8, "1.50000000"}, {"0.123456", 6, "0.123456"},
	}
	for _, c := range cases {
		if got := MustParse(c.in).StringFixed(c.places); got != c.want {
			t.Errorf("StringFixed(%s, %d) = %s; want %s", c.in, c.places, got, c.want)
		}
	}
}

func TestZeroAndDivInt(t *testing.T) {
	if Zero != (Decimal{}) || !Zero.IsZero() || Zero.String() != "0" {
		t.Error("Zero is not 0")
	}
	cases := []struct {
		in   string
		n    int64
		mode RoundingMode
		want string
	}{
		{"100", 3, RoundHalfUp, "33.333333"},
		{"-100", 3, RoundHalfUp, "-33.333333"},
		{"100", -3, RoundHalfUp, "-33.333333"},
		{"200", 3, RoundHalfUp, "66.666667"},
		{"10", 4, RoundHalfUp, "2.5"},
		{"0.000001", 2, RoundHalfUp, "0.000001"},
		{"0.000001", 2, RoundHalfEven, "0"},
		{"0.000003", 2, RoundHalfEven, "0.000002"},
		{"-0.000001", 2, RoundHalfUp, "-0.000001"},
		{"1000", 1, RoundHalfUp, "1000"},
		{"-9223372036854.775808", 1, RoundHalfUp, "-9223372036854.775808"},
	}
	for _, c := range cases {
		if got := MustParse(c.in).DivIntRound(c.n, c.mode).String(); got != c.want {
			t.Errorf("%s.DivIntRound(%d, %d) = %s; want %s", c.in, c.n, c.mode, got, c.want)
		}
	}
	if got := MustParse("100").DivInt(3).String(); got != "33.333333" {
		t.Errorf("DivInt = %s", got)
	}
	mustPanic := func(name string, want error, f func()) {
		defer func() {
			if recover() != want {
				t.Errorf("%s: expected panic %v", name, want)
			}
		}()
		f()
	}
	mustPanic("DivInt(0)", ErrDivisionByZero, func() { MustParse("1").DivInt(0) })
	mustPanic("MinInt64/-1", ErrOverflow, func() { Decimal{math.MinInt64}.DivInt(-1) })
}

func TestNew(t *testing.T) {
	cases := []struct {
		m      int64
		places int
		want   string
	}{
		{371, 2, "3.71"}, {-5, 2, "-0.05"}, {1, 6, "0.000001"}, {81, 3, "0.081"},
		{42, 0, "42"}, {0, 3, "0"}, {1500, 2, "15"},
		{math.MaxInt64, 6, "9223372036854.775807"}, {math.MinInt64, 6, "-9223372036854.775808"},
	}
	for _, c := range cases {
		d, err := New(c.m, c.places)
		if err != nil || d.String() != c.want {
			t.Errorf("New(%d, %d) = %v, %v; want %s", c.m, c.places, d, err, c.want)
		}
		if d != MustParse(c.want) {
			t.Errorf("New(%d, %d) != MustParse(%q)", c.m, c.places, c.want)
		}
	}
	for _, c := range []struct {
		m      int64
		places int
		err    error
	}{
		{1, 7, ErrPlaces}, {1, -1, ErrPlaces}, {math.MaxInt64, 0, ErrOverflow}, {math.MinInt64, 1, ErrOverflow},
	} {
		if _, err := New(c.m, c.places); err != c.err {
			t.Errorf("New(%d, %d) error = %v; want %v", c.m, c.places, err, c.err)
		}
	}
}

var benchRate = MustParse("3.71")
var sink Decimal

func BenchmarkMulMustParse(b *testing.B) {
	v := MustParse("1234.50")
	for i := 0; i < b.N; i++ {
		sink = v.Mul(MustParse("3.71"))
	}
}

func BenchmarkMulMustNew(b *testing.B) {
	v := MustParse("1234.50")
	for i := 0; i < b.N; i++ {
		sink = v.Mul(MustNew(371, 2))
	}
}

func BenchmarkMulPackageVar(b *testing.B) {
	v := MustParse("1234.50")
	for i := 0; i < b.N; i++ {
		sink = v.Mul(benchRate)
	}
}

func TestMustFunctions(t *testing.T) {
	if got := MustNewInt(42).String(); got != "42" {
		t.Errorf("MustFromInt = %s", got)
	}
	if got := MustParseRound("8.1234565", RoundHalfEven).String(); got != "8.123456" {
		t.Errorf("MustParseRound = %s", got)
	}
	mustPanic := func(name string, f func()) {
		defer func() {
			if recover() == nil {
				t.Errorf("%s should panic", name)
			}
		}()
		f()
	}
	mustPanic("MustFromInt", func() { MustNewInt(math.MaxInt64) })
	mustPanic("MustParseRound", func() { MustParseRound("abc", RoundHalfUp) })
}

func TestStringGrouped(t *testing.T) {
	cases := []struct {
		in     string
		places int
		sep    string
		want   string
	}{
		{"1238832.25", 2, "'", "1'238'832.25"},
		{"-1238832.25", 2, "'", "-1'238'832.25"},
		{"999", 2, "'", "999.00"},
		{"1000", 2, "'", "1'000.00"},
		{"-1000", 0, "'", "-1'000"},
		{"123456", 0, "'", "123'456"},
		{"1234567.891", 2, "’", "1’234’567.89"},
		{"1234567.5", 1, " ", "1 234 567.5"},
		{"1234567", 2, "", "1234567.00"},
		{"0.5", 2, "'", "0.50"},
		{"999999.995", 2, "'", "1'000'000.00"}, // rounding carries into a new group
		{"-0.001", 2, "'", "0.00"},
		{"9223372036854.775807", 6, ",", "9,223,372,036,854.775807"},
		{"-9223372036854.775808", 6, ",", "-9,223,372,036,854.775808"},
	}
	for _, c := range cases {
		if got := MustParse(c.in).StringGrouped(c.places, c.sep); got != c.want {
			t.Errorf("StringGrouped(%s, %d, %q) = %q; want %q", c.in, c.places, c.sep, got, c.want)
		}
	}
}

type invoice struct {
	Net  Decimal     `json:"net"`
	Rate Decimal     `json:"rate"`
	Disc NullDecimal `json:"disc"`
}

func TestJSON(t *testing.T) {
	in := invoice{Net: MustParse("1234.5"), Rate: MustParse("0.081")}
	b, err := json.Marshal(in)
	if err != nil || string(b) != `{"net":"1234.5","rate":"0.081","disc":null}` {
		t.Fatalf("Marshal = %s, %v", b, err)
	}
	var out invoice
	if err := json.Unmarshal([]byte(`{"net":1234.5,"rate":"0.081","disc":"2.50"}`), &out); err != nil {
		t.Fatal(err)
	}
	if out.Net != in.Net || out.Rate != in.Rate || !out.Disc.Valid || out.Disc.Decimal.String() != "2.5" {
		t.Errorf("Unmarshal = %+v", out)
	}
	for _, bad := range []string{`{"net":1e3}`, `{"net":"abc"}`, `{"net":true}`, `{"net":"0.0000001"}`} {
		if err := json.Unmarshal([]byte(bad), &out); err == nil {
			t.Errorf("Unmarshal(%s) should fail", bad)
		}
	}
}

func TestScanValue(t *testing.T) {
	var d Decimal
	for _, c := range []struct {
		src  any
		want string
	}{
		{[]byte("8.10"), "8.1"}, {"-3", "-3"}, {int64(42), "42"}, {8.1, "8.1"},
	} {
		if err := d.Scan(c.src); err != nil || d.String() != c.want {
			t.Errorf("Scan(%v) = %v, %v; want %s", c.src, d, err, c.want)
		}
	}
	if d.Scan(nil) == nil || d.Scan(true) == nil || d.Scan(float64(float32(8.1))) == nil {
		t.Error("expected errors for NULL, bool and float32-widened values")
	}
	v, _ := MustParse("8.1").Value()
	if v != "8.1" {
		t.Errorf("Value = %v", v)
	}
	var n NullDecimal
	if err := n.Scan(nil); err != nil || n.Valid {
		t.Error("NullDecimal NULL")
	}
	if err := n.Scan("1.5"); err != nil || !n.Valid || n.Decimal.String() != "1.5" {
		t.Error("NullDecimal value")
	}
}

// ---------------------------------------------------------------------------
// Cross-check against math/big
// ---------------------------------------------------------------------------

var modes = []RoundingMode{RoundHalfUp, RoundHalfEven, RoundDown, RoundUp, RoundFloor, RoundCeiling}

// refDiv computes round(num/den) with an independent big.Int implementation.
func refDiv(num, den *big.Int, mode RoundingMode) *big.Int {
	neg := num.Sign()*den.Sign() < 0
	n, d := new(big.Int).Abs(num), new(big.Int).Abs(den)
	q, r := new(big.Int).QuoRem(n, d, new(big.Int))
	if r.Sign() != 0 {
		twice := new(big.Int).Lsh(r, 1)
		cmp := twice.Cmp(d) // <0 below half, 0 tie, >0 above
		up := false
		switch mode {
		case RoundHalfUp:
			up = cmp >= 0
		case RoundHalfEven:
			up = cmp > 0 || (cmp == 0 && q.Bit(0) == 1)
		case RoundUp:
			up = true
		case RoundFloor:
			up = neg
		case RoundCeiling:
			up = !neg
		default:
			up = cmp >= 0
		}
		if up {
			q.Add(q, big.NewInt(1))
		}
	}
	if neg {
		q.Neg(q)
	}
	return q
}

func fits(x *big.Int) bool { return x.IsInt64() }

// expect checks f against the reference result (panics iff it does not fit).
func expect(t *testing.T, what string, want *big.Int, f func() Decimal) {
	t.Helper()
	var got Decimal
	var pan any
	func() {
		defer func() { pan = recover() }()
		got = f()
	}()
	if !fits(want) {
		if pan != ErrOverflow {
			t.Errorf("%s: want overflow panic, got %v (panic %v)", what, got, pan)
		}
		return
	}
	if pan != nil || got.value != want.Int64() {
		t.Errorf("%s: got %d (panic %v), want %s", what, got.value, pan, want)
	}
}

func randVal(r *rand.Rand) int64 {
	switch r.Intn(5) {
	case 0:
		return int64(r.Intn(2001) - 1000)
	case 1:
		return r.Int63n(100_000_000_000) - 50_000_000_000
	case 2:
		return []int64{0, 1, -1, 500_000, -500_000, math.MaxInt64, math.MinInt64, math.MaxInt64 - 1, math.MinInt64 + 1}[r.Intn(9)]
	case 3:
		return int64(r.Uint64())
	}
	return (r.Int63n(2_000_000) - 1_000_000) * 500_000 // lots of ties
}

func TestAgainstBig(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	big1M := big.NewInt(scale)
	for i := 0; i < 300_000; i++ {
		a, b := randVal(r), randVal(r)
		mode := modes[r.Intn(len(modes))]
		A, B := Decimal{a}, Decimal{b}
		ba, bb := big.NewInt(a), big.NewInt(b)

		expect(t, "Add", new(big.Int).Add(ba, bb), func() Decimal { return A.Add(B) })
		expect(t, "Sub", new(big.Int).Sub(ba, bb), func() Decimal { return A.Sub(B) })
		expect(t, "MulInt", new(big.Int).Mul(ba, bb), func() Decimal { return A.MulInt(b) })
		expect(t, "Mul", refDiv(new(big.Int).Mul(ba, bb), big1M, mode), func() Decimal { return A.MulRound(B, mode) })
		if b != 0 {
			expect(t, "Div", refDiv(new(big.Int).Mul(ba, big1M), bb, mode), func() Decimal { return A.DivRound(B, mode) })
		}
		if b != 0 {
			expect(t, "DivInt", refDiv(ba, bb, mode), func() Decimal { return A.DivIntRound(b, mode) })
		}
		if b != 0 {
			step := new(big.Int).Abs(bb)
			q := refDiv(ba, step, mode)
			expect(t, "RoundTo", q.Mul(q, step), func() Decimal { return A.RoundTo(B, mode) })
		}
		places := r.Intn(7)
		unit := big.NewInt(int64(pow10[scaleDigits-places]))
		q := refDiv(ba, unit, mode)
		expect(t, "Round", q.Mul(q, unit), func() Decimal { return A.Round(places, mode) })

		// String <-> Parse round trip
		if got, err := Parse(A.String()); err != nil || got != A {
			t.Fatalf("round trip %d: %v, %v", a, got, err)
		}
	}
}

func TestDivisionByZero(t *testing.T) {
	defer func() {
		if recover() != ErrDivisionByZero {
			t.Error("expected ErrDivisionByZero")
		}
	}()
	MustParse("1").Div(Decimal{})
}
