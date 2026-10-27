// Package decimal implements a fixed-point decimal type for money and rates.
//
// A Decimal stores its value as an int64 scaled by 10^6 (six decimal places).
// All arithmetic is exact integer arithmetic; floats are never involved.
// The range is about ±9.22 trillion with a resolution of 0.000001.
//
// Arithmetic methods panic on overflow (ErrOverflow) and on division by zero
// (ErrDivisionByZero). Parsing, JSON and SQL decoding return errors instead.
package decimal

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/bits"
	"strconv"
	"strings"
)

const scaleDigits = 6
const scale = 1_000_000 // 10^scaleDigits

var pow10 = [...]uint64{1, 10, 100, 1_000, 10_000, 100_000, 1_000_000}

var ErrSyntax = errors.New("invalid decimal syntax")
var ErrOverflow = errors.New("decimal overflow")
var ErrPrecision = errors.New("decimal has more than 6 fractional digits")
var ErrDivisionByZero = errors.New("decimal division by zero")
var ErrPlaces = errors.New("decimal places out of range 0..6")

// RoundingMode selects how a value is rounded when digits are discarded.
// The zero value is RoundHalfUp.
type RoundingMode int

const (
	// RoundHalfUp rounds to the nearest value; ties go away from zero
	// (2.5 -> 3, -2.5 -> -3). This is the usual commercial rounding.
	RoundHalfUp RoundingMode = iota
	// RoundHalfEven rounds to the nearest value; ties go to the even
	// neighbor (2.5 -> 2, 3.5 -> 4), a.k.a. banker's rounding.
	RoundHalfEven
	// RoundDown truncates toward zero.
	RoundDown
	// RoundUp rounds away from zero.
	RoundUp
	// RoundFloor rounds toward negative infinity.
	RoundFloor
	// RoundCeiling rounds toward positive infinity.
	RoundCeiling
)

// Decimal is a fixed-point number with six decimal places.
// The zero value is 0. Decimals are comparable with ==.
type Decimal struct {
	value int64 // actual value * scale
}

// Zero is the Decimal 0. It equals the zero value of Decimal.
var Zero Decimal

// ───── Construction and parsing ────────────────────────────────────────────

// New returns mantissa / 10^places, e.g. New(371, 2) is 3.71 and
// New(-5, 2) is -0.05. It is an inexpensive multiplication (no string parsing)
// and fails with ErrPlaces unless 0 <= places <= 6, or ErrOverflow if too large.
func New(mantissa int64, places int) (Decimal, error) {
	if places < 0 || places > scaleDigits {
		return Decimal{}, ErrPlaces
	}
	f := int64(pow10[scaleDigits-places])
	if mantissa > math.MaxInt64/f || mantissa < math.MinInt64/f {
		return Decimal{}, ErrOverflow
	}
	return Decimal{mantissa * f}, nil
}

// NewInt returns n as a Decimal. It fails if n is too large.
func NewInt(n int64) (Decimal, error) {
	if n > math.MaxInt64/scale || n < math.MinInt64/scale {
		return Decimal{}, ErrOverflow
	}
	return Decimal{n * scale}, nil
}

// MustNew is like New but panics on error. Meant for constants and tests.
func MustNew(mantissa int64, places int) Decimal {
	d, err := New(mantissa, places)
	if err != nil {
		panic(err)
	}
	return d
}

// MustNewInt is like NewInt but panics on error. Meant for constants and tests.
func MustNewInt(n int64) Decimal {
	d, err := NewInt(n)
	if err != nil {
		panic(err)
	}
	return d
}

// Parse parses a plain decimal string such as "8.1", "-0.05" or "1200".
// A leading '+' or '-' is allowed; exponents, spaces, and separators are not.
// Fractional digits beyond the sixth are only accepted if they are all zeros;
// otherwise ErrPrecision is returned. Use ParseRound to round instead.
func Parse(s string) (Decimal, error) {
	return parse(s, RoundHalfUp, true)
}

// ParseRound is like Parse but rounds excess fractional digits using mode.
func ParseRound(s string, mode RoundingMode) (Decimal, error) {
	return parse(s, mode, false)
}

// MustParse is like Parse but panics on error. Meant for constants and tests.
func MustParse(s string) Decimal {
	d, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}

// MustParseRound is like ParseRound but panics on error. Meant for constants and tests.
func MustParseRound(s string, mode RoundingMode) Decimal {
	d, err := ParseRound(s, mode)
	if err != nil {
		panic(err)
	}
	return d
}

func parse(s string, mode RoundingMode, strict bool) (Decimal, error) {
	fail := func(err error) (Decimal, error) {
		return Decimal{}, fmt.Errorf("decimal: parsing %q: %w", s, err)
	}

	i, neg := 0, false
	if len(s) > 0 && (s[0] == '+' || s[0] == '-') {
		neg = s[0] == '-'
		i++
	}

	digits := 0
	var ip uint64 // integer part
	for ; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		d := uint64(s[i] - '0')
		if ip > (math.MaxUint64-d)/10 {
			return fail(ErrOverflow)
		}
		ip = ip*10 + d
		digits++
	}

	var frac uint64 // fractional part, scaled to exactly scaleDigits digits
	nFrac := 0
	firstExtra, restNonZero := -1, false // digits beyond the sixth place
	if i < len(s) && s[i] == '.' {
		i++
		for ; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
			d := int(s[i] - '0')
			digits++
			switch {
			case nFrac < scaleDigits:
				frac = frac*10 + uint64(d)
				nFrac++
			case firstExtra < 0:
				firstExtra = d
			case d != 0:
				restNonZero = true
			}
		}
	}
	if digits == 0 || i != len(s) {
		return fail(ErrSyntax)
	}
	for ; nFrac < scaleDigits; nFrac++ {
		frac *= 10
	}

	rem := remNone
	switch {
	case firstExtra < 0:
	case firstExtra == 0:
		if restNonZero {
			rem = remBelow
		}
	case firstExtra < 5:
		rem = remBelow
	case firstExtra == 5:
		rem = remTie
		if restNonZero {
			rem = remAbove
		}
	default:
		rem = remAbove
	}
	if rem != remNone && strict {
		return fail(ErrPrecision)
	}

	if ip > (math.MaxUint64-frac)/scale {
		return fail(ErrOverflow)
	}
	mag := ip*scale + frac
	if increment(mode, rem, neg, mag&1 == 1) {
		mag++
		if mag == 0 {
			return fail(ErrOverflow)
		}
	}
	v, ok := toInt64(mag, neg)
	if !ok {
		return fail(ErrOverflow)
	}
	return Decimal{v}, nil
}

// ───── Arithmetic ──────────────────────────────────────────────────────────

// Add returns d + o.
func (d Decimal) Add(o Decimal) Decimal {
	s := d.value + o.value
	if (d.value^s)&(o.value^s) < 0 {
		panic(ErrOverflow)
	}
	return Decimal{s}
}

// Sub returns d - o.
func (d Decimal) Sub(o Decimal) Decimal {
	s := d.value - o.value
	if (d.value^o.value)&(d.value^s) < 0 {
		panic(ErrOverflow)
	}
	return Decimal{s}
}

// Neg returns -d.
func (d Decimal) Neg() Decimal {
	if d.value == math.MinInt64 {
		panic(ErrOverflow)
	}
	return Decimal{-d.value}
}

// Abs returns |d|.
func (d Decimal) Abs() Decimal {
	if d.value < 0 {
		return d.Neg()
	}
	return d
}

// Mul returns d * o, rounded half-up at the sixth decimal place.
// For typical amounts times rates the result is exact; round explicitly
// afterwards with Round or RoundTo.
func (d Decimal) Mul(o Decimal) Decimal { return d.MulRound(o, RoundHalfUp) }

// MulRound returns d * o, rounded at the sixth decimal place using mode.
func (d Decimal) MulRound(o Decimal, mode RoundingMode) Decimal {
	neg := (d.value < 0) != (o.value < 0)
	// (|a| * |b|) / scale, with a 128-bit intermediate product.
	mag, ok := mulDiv(abs(d.value), abs(o.value), scale, neg, mode)
	return fromMag(mag, neg, ok)
}

// MulInt returns d * n exactly (e.g. unit price times quantity).
func (d Decimal) MulInt(n int64) Decimal {
	hi, lo := bits.Mul64(abs(d.value), abs(n))
	v, ok := toInt64(lo, (d.value < 0) != (n < 0))
	if hi != 0 || !ok {
		panic(ErrOverflow)
	}
	return Decimal{v}
}

// Div returns d / o, rounded half-up at the sixth decimal place.
func (d Decimal) Div(o Decimal) Decimal { return d.DivRound(o, RoundHalfUp) }

// DivRound returns d / o, rounded at the sixth decimal place using mode.
func (d Decimal) DivRound(o Decimal, mode RoundingMode) Decimal {
	if o.value == 0 {
		panic(ErrDivisionByZero)
	}
	neg := (d.value < 0) != (o.value < 0)
	// (|a| * scale) / |b|, with a 128-bit intermediate numerator.
	mag, ok := mulDiv(abs(d.value), scale, abs(o.value), neg, mode)
	return fromMag(mag, neg, ok)
}

// DivInt returns d / n, rounded half-up at the sixth decimal place
// (e.g. an amount split into n equal parts).
func (d Decimal) DivInt(n int64) Decimal { return d.DivIntRound(n, RoundHalfUp) }

// DivIntRound returns d / n, rounded at the sixth decimal place using mode.
func (d Decimal) DivIntRound(n int64, mode RoundingMode) Decimal {
	if n == 0 {
		panic(ErrDivisionByZero)
	}
	neg := (d.value < 0) != (n < 0)
	// |value| / |n|; the quotient never exceeds |value|, so only the
	// sign conversion (MinInt64 / -1) can overflow.
	mag, ok := mulDiv(abs(d.value), 1, abs(n), neg, mode)
	return fromMag(mag, neg, ok)
}

// Round rounds d to the given number of decimal places (0 to 6) using mode.
func (d Decimal) Round(places int, mode RoundingMode) Decimal {
	if places < 0 {
		panic("decimal: negative number of places")
	}
	if places >= scaleDigits {
		return d
	}
	return Decimal{roundToMultiple(d.value, pow10[scaleDigits-places], mode)}
}

// RoundTo rounds d to the nearest multiple of step using mode.
//
// Example (applying Swiss 5-Rappen rounding:
// RoundTo(MustParse("0.05"), RoundHalfUp)
func (d Decimal) RoundTo(step Decimal, mode RoundingMode) Decimal {
	if step.value == 0 {
		panic(ErrDivisionByZero)
	}
	return Decimal{roundToMultiple(d.value, abs(step.value), mode)}
}

// ───── Comparison ──────────────────────────────────────────────────────────

// Cmp returns -1, 0, or +1 if d is less than, equal to, or greater than o.
func (d Decimal) Cmp(o Decimal) int {
	switch {
	case d.value < o.value:
		return -1
	case d.value > o.value:
		return 1
	}
	return 0
}

// Sign returns -1, 0. or +1 depending on the sign of d.
func (d Decimal) Sign() int { return d.Cmp(Zero) }

// IsZero reports whether d is 0.
func (d Decimal) IsZero() bool { return d.value == 0 }

// ───── Formatting ──────────────────────────────────────────────────────────

// String returns the shortest exact representation, e.g. "8.1", "-0.05", "100".
func (d Decimal) String() string { return format(d.value, -1) }

// StringFixed rounds d half-up to the given number of places and formats it
// with exactly that many fractional digits, e.g. "1234.50" for places = 2.
func (d Decimal) StringFixed(places int) string {
	return format(d.Round(places, RoundHalfUp).value, places)
}

// StringGrouped is like StringFixed but also inserts sep between groups of
// three integer digits, e.g. StringGrouped(2, "'") yields "1'238'832.25".
// sep may be any string ("'", "’", " ", "."); an empty sep disables grouping.
// The decimal point is always ".".
func (d Decimal) StringGrouped(places int, sep string) string {
	return groupThousands(d.StringFixed(places), sep)
}

// groupThousands inserts sep into the integer part of a formatted number.
func groupThousands(s, sep string) string {
	start := 0
	if s[0] == '-' {
		start = 1
	}
	end := strings.IndexByte(s, '.')
	if end < 0 {
		end = len(s)
	}
	digits := s[start:end]
	if sep == "" || len(digits) <= 3 {
		return s
	}

	var b strings.Builder
	b.Grow(len(s) + (len(digits)-1)/3*len(sep))
	b.WriteString(s[:start])
	first := len(digits) % 3
	if first == 0 {
		first = 3
	}
	b.WriteString(digits[:first])
	for i := first; i < len(digits); i += 3 {
		b.WriteString(sep)
		b.WriteString(digits[i : i+3])
	}
	b.WriteString(s[end:])
	return b.String()
}

func format(v int64, places int) string {
	u := abs(v)
	ip, fp := u/scale, u%scale

	var fs [scaleDigits]byte
	for i := scaleDigits - 1; i >= 0; i-- {
		fs[i] = '0' + byte(fp%10)
		fp /= 10
	}
	n := places
	if n < 0 { // trim trailing zeros
		for n = scaleDigits; n > 0 && fs[n-1] == '0'; n-- {
		}
	}

	buf := make([]byte, 0, 32)
	if v < 0 {
		buf = append(buf, '-')
	}
	buf = strconv.AppendUint(buf, ip, 10)
	if n > 0 {
		buf = append(buf, '.')
		if n <= scaleDigits {
			buf = append(buf, fs[:n]...)
		} else {
			buf = append(buf, fs[:]...)
			for k := scaleDigits; k < n; k++ {
				buf = append(buf, '0')
			}
		}
	}
	return string(buf)
}

// ───── Text and JSON ───────────────────────────────────────────────────────

// MarshalText implements encoding.TextMarshaler.
func (d Decimal) MarshalText() ([]byte, error) { return []byte(d.String()), nil }

// UnmarshalText implements encoding.TextUnmarshaler. It is strict, see Parse.
func (d *Decimal) UnmarshalText(text []byte) error {
	v, err := Parse(string(text))
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// MarshalJSON encodes d as a JSON string ("8.1") so that JavaScript clients
// do not convert it to a float.
func (d Decimal) MarshalJSON() ([]byte, error) {
	return []byte(`"` + d.String() + `"`), nil
}

// UnmarshalJSON accepts a JSON string ("8.1") or a plain JSON number (8.1).
// Numbers are parsed from their text, never via float64. JSON null is a no-op.
func (d *Decimal) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		return d.UnmarshalText([]byte(s))
	}
	return d.UnmarshalText(data)
}

// ───── database/sql ────────────────────────────────────────────────────────

// Scan implements sql.Scanner. DECIMAL columns arrive as []byte or string and
// are parsed from text. Integers are accepted. float64 values (e.g. SQLite
// REAL, MySQL DOUBLE) are converted via their shortest decimal representation
// and rejected with ErrPrecision if that has more than 6 fractional digits.
// NULL is an error; use NullDecimal for nullable columns.
func (d *Decimal) Scan(src any) error {
	var (
		v   Decimal
		err error
	)
	switch s := src.(type) {
	case []byte:
		v, err = Parse(string(s))
	case string:
		v, err = Parse(s)
	case int64:
		v, err = NewInt(s)
	case float64:
		v, err = Parse(strconv.FormatFloat(s, 'f', -1, 64))
	case nil:
		return errors.New("decimal: cannot scan NULL into Decimal, use NullDecimal")
	default:
		return fmt.Errorf("decimal: cannot scan %T into Decimal", src)
	}
	if err != nil {
		return err
	}
	*d = v
	return nil
}

// Value implements driver.Valuer. The value is sent as a string, which MySQL,
// MariaDB, PostgreSQL, and SQLite all convert to DECIMAL/NUMERIC exactly.
func (d Decimal) Value() (driver.Value, error) { return d.String(), nil }

// NullDecimal is a Decimal that may be NULL in the database or null in JSON.
type NullDecimal struct {
	Decimal Decimal
	Valid   bool // false if NULL
}

// Scan implements sql.Scanner.
func (n *NullDecimal) Scan(src any) error {
	if src == nil {
		*n = NullDecimal{}
		return nil
	}
	if err := n.Decimal.Scan(src); err != nil {
		*n = NullDecimal{}
		return err
	}
	n.Valid = true
	return nil
}

// Value implements driver.Valuer.
func (n NullDecimal) Value() (driver.Value, error) {
	if !n.Valid {
		return nil, nil
	}
	return n.Decimal.Value()
}

// MarshalJSON implements json.Marshaler.
func (n NullDecimal) MarshalJSON() ([]byte, error) {
	if !n.Valid {
		return []byte("null"), nil
	}
	return n.Decimal.MarshalJSON()
}

// UnmarshalJSON implements json.Unmarshaler.
func (n *NullDecimal) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		*n = NullDecimal{}
		return nil
	}
	if err := n.Decimal.UnmarshalJSON(data); err != nil {
		return err
	}
	n.Valid = true
	return nil
}

// ───── Internals ───────────────────────────────────────────────────────────

// rem classifies a division remainder relative to half of the divisor.
type rem int

const (
	remNone  rem = iota // remainder is exactly zero
	remBelow            // 0 < remainder < half
	remTie              // remainder == half
	remAbove            // remainder > half
)

func remClass(r, den uint64) rem {
	if r == 0 {
		return remNone
	}
	h := den - r // compare r with den-r instead of 2r to avoid overflow
	switch {
	case r < h:
		return remBelow
	case r == h:
		return remTie
	}
	return remAbove
}

// increment reports whether the truncated magnitude must be increased by one
// unit. neg is the sign of the value, odd tells if the truncated magnitude is odd.
func increment(mode RoundingMode, r rem, neg, odd bool) bool {
	if r == remNone {
		return false
	}
	switch mode {
	case RoundHalfUp:
		return r >= remTie
	case RoundHalfEven:
		return r == remAbove || (r == remTie && odd)
	case RoundDown:
		return false
	case RoundUp:
		return true
	case RoundFloor:
		return neg
	case RoundCeiling:
		return !neg
	}
	panic("decimal: invalid rounding mode")
}

// mulDiv computes round(x*y/den) using a 128-bit intermediate product.
// ok is false if the quotient does not fit into 64 bits.
func mulDiv(x, y, den uint64, neg bool, mode RoundingMode) (q uint64, ok bool) {
	hi, lo := bits.Mul64(x, y)
	if hi >= den { // quotient would overflow; bits.Div64 would panic
		return 0, false
	}
	q, r := bits.Div64(hi, lo, den)
	if increment(mode, remClass(r, den), neg, q&1 == 1) {
		q++
		if q == 0 {
			return 0, false
		}
	}
	return q, true
}

// roundToMultiple rounds v to a multiple of unit (unit > 0).
func roundToMultiple(v int64, unit uint64, mode RoundingMode) int64 {
	neg := v < 0
	u := abs(v)
	q, r := u/unit, u%unit
	if increment(mode, remClass(r, unit), neg, q&1 == 1) {
		q++
	}
	hi, lo := bits.Mul64(q, unit)
	res, ok := toInt64(lo, neg)
	if hi != 0 || !ok {
		panic(ErrOverflow)
	}
	return res
}

func fromMag(mag uint64, neg, ok bool) Decimal {
	if ok {
		if v, fits := toInt64(mag, neg); fits {
			return Decimal{v}
		}
	}
	panic(ErrOverflow)
}

// abs returns |v| as uint64; it is also correct for math.MinInt64.
func abs(v int64) uint64 {
	if v < 0 {
		return uint64(-v)
	}
	return uint64(v)
}

// toInt64 applies the sign to a magnitude and reports whether it fits.
func toInt64(mag uint64, neg bool) (int64, bool) {
	if neg {
		if mag > 1<<63 {
			return 0, false
		}
		return int64(-mag), true
	}
	if mag > math.MaxInt64 {
		return 0, false
	}
	return int64(mag), true
}
