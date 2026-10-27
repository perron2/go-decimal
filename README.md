Go Decimal Type
===============

A Go package for fast fixed-point decimals.

Floats can't represent most decimal fractions (`8.1` becomes `8.100000381469727`
after a float32 round trip). This package stores a value as an `int64` scaled by
10^6, so all arithmetic is exact integer arithmetic and rounding is always an
explicit decision.

* **Six decimal places**, range of about ±9.22 trillion.
* **Same size as an `int64`**, comparable with `==`, usable as a map key, zero
  value is `0`.
* **Encapsulated**: the field is unexported, so values can only be created and
  combined through the API.
* **No dependencies**, only the standard library (`math/bits` for the 128-bit
  intermediate products in `Mul` and `Div`).
* JSON, `database/sql`, and `encoding.TextMarshaler` support included.

Install
-------
```sh
go get github.com/perron2/go-decimal
```

Requires Go 1.25+.

Quick Start
-----------

```go
import "github.com/perron2/decimal"

net := decimal.MustParse("1234.50")
vatRate := decimal.MustParse("0.081") // 8.1 %

vat := net.Mul(vatRate).Round(2, decimal.RoundHalfUp) // 99.9945 -> 99.99
total := net.Add(vat) // 1334.49

fmt.Println(total) // 1334.49
fmt.Println(total.StringGrouped(2, "'")) // 1'334.49
fmt.Println(total.RoundTo(decimal.MustParse("0.05"), 0)) // 1334.5 (5-Rappen rounding)
```

Creating values
---------------

|Function|Example|Notes|
|-|-|-|
|`Parse(s)`|`Parse("8.1")`|Strict: more than 6 fractional digits (other than zeros) is `ErrPrecision`|
|`ParseRound(s, mode)`|`ParseRound("8.1234565", RoundHalfEven)`|Rounds excess digits|
|`New(mantissa, places)`|`New(371, 2)` is 3.71|No string parsing, `places` 0..6|
|`NewInt(n)`|`NewInt(100)`||
|`Zero`|`decimal.Zero`|The value 0|

Each has a `Must…` variant (`MustParse`, `MustParseRound`, `MustNew`,
`MustNewInt`) that panics instead of returning an error. Use them for constants
and tests, not for user input.

There is deliberately no conversion from `float64`.

For constants used repeatedly, declare them once at package level, which costs
nothing per call:

```go
var vatRate = decimal.MustParse("0.081")
```

Arithmetic
----------

```go
a.Add(b)  a.Sub(b)  a.Neg()  a.Abs()

a.MulInt(3)                  // exact, e.g. unit price x quantity
a.Mul(b)                     // rounded half-up at the 6th decimal place
a.MulRound(b, mode)          // same with an explicit rounding mode

a.Div(b)   a.DivRound(b, mode)
a.DivInt(3) a.DivIntRound(3, mode)

a.Round(2, mode)             // round to 0..6 decimal places
a.RoundTo(step, mode)        // round to a multiple of step, e.g. 0.05
```

For typical "amount x rate" calculations `Mul` is exact (amounts have 2 and
rates 3 decimals), so the one rounding step you need is the explicit
`Round(2, …)` afterwards.

Overflow and division by zero **panic** with `ErrOverflow` /
`ErrDivisionByZero`. Parsing, JSON and SQL decoding return errors instead.

Rounding Modes
--------------

The zero value of `RoundingMode` is `RoundHalfUp`.

|Mode|2.5|-2.5|Description|
|-|-|-|-|
|`RoundHalfUp`|3|-3|Nearest, ties away from zero (commercial rounding)|
|`RoundHalfEven`|2|-2|Nearest, ties to even (banker's rounding)|
|`RoundDown`|2|-2|Toward zero (truncate)|
|`RoundUp`|3|-3|Away from zero|
|`RoundFloor`|2|-3|Toward negative infinity|
|`RoundCeiling`|3|-2|Toward positive infinity|

Comparison
----------

```go
a == b                       // equality, a Decimal has one canonical representation
a.Cmp(b) > 0                 // a > b; returns -1, 0 or +1
a.Sign() > 0                 // compare with zero: -1, 0 or +1
a.IsZero()
```

Formatting
----------

```go
d := decimal.MustParse("1238832.25")

d.String()                   // "1238832.25" (shortest exact form, "8.1", "100")
d.StringFixed(2)             // "1238832.25" (exactly 2 places, rounded half-up)
d.StringGrouped(2, "'")      // "1'238'832.25"
d.StringGrouped(0, " ")      // "1 238 832"
```

JSON
----

Values are written as JSON **strings** so JavaScript clients don't turn them
into floats. Both strings and plain numbers are accepted when reading, and
numbers are parsed from their text, never via `float64`. Exponents (`1e3`) and
excess precision are rejected.

```go
type Invoice struct {
    Net  decimal.Decimal     `json:"net"`
    Rate decimal.Decimal     `json:"rate"`
    Disc decimal.NullDecimal `json:"disc"` // may be null
}
// {"net":"1234.5","rate":"0.081","disc":null}
```

SQL
---

`Decimal` implements `sql.Scanner` and `driver.Valuer`. Use `DECIMAL(p,s)`
columns (never `FLOAT` or `DOUBLE`) and scan straight into the type:

```go
var amount decimal.Decimal
err := db.QueryRow(`SELECT amount FROM invoices WHERE id = ?`, id).Scan(\&amount)

\_, err = db.Exec(`UPDATE invoices SET amount = ? WHERE id = ?`, amount, id)
```

* `DECIMAL` columns arrive as text and are parsed exactly. Integers are accepted
  too.
* `float64` values (SQLite `REAL`, MySQL `DOUBLE`) are converted via their
  shortest representation, and rejected with `ErrPrecision` if that has more
  than 6 decimals. A widened `float32` such as `8.100000381469727` is therefore
  an error instead of silently stored.
* `NULL` is an error for `Decimal`. Use `NullDecimal` for nullable columns.
* Values are written as strings, which MySQL, MariaDB, PostgreSQL and SQLite
  convert exactly.

Errors
------

|Error|Returned or panicked when|
|-|-|
|`ErrSyntax`|Input is not a plain decimal number|
|`ErrPrecision`|More than 6 fractional digits (non-zero), strict parsing only|
|`ErrOverflow`|Result outside about ±9.22 trillion|
|`ErrDivisionByZero`|Division by zero (panic)|
|`ErrPlaces`|`New` called with `places` outside 0..6|

Parse errors wrap the sentinel with the input, so use
`errors.Is(err, decimal.ErrPrecision)`.

Limitations
-----------

* The scale is fixed at six places. For more (e.g. exchange rates with 8+
  decimals) change the `scale`, `scaleDigits` and `pow10` definitions, which
  reduces the range accordingly.
* `Div` and `DivInt` can't be exact. When splitting an amount into parts, handle
  the remainder yourself so the parts add up to the total.

Tests
-----

```
go test ./...
go test -bench . ./...
```

The tests include a randomized cross-check of `Add`, `Sub`, `Mul`, `MulInt`,
`Div`, `DivInt`, `Round` and `RoundTo` against `math/big` for all rounding
modes, including overflow and `MinInt64` edge cases.

