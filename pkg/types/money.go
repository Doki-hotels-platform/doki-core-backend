package types

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// Money represents a monetary amount stored in its minor currency unit (e.g. cents, santim)
// to prevent floating-point arithmetic rounding errors.
type Money struct {
	AmountMinor int64  `json:"amount_minor"`
	Currency    string `json:"currency"` // ISO 4217 code (e.g., "ETB", "USD")
}

// NewMoney creates a new Money value object. Default currency is "ETB" if empty.
func NewMoney(amountMinor int64, currency string) Money {
	if currency == "" {
		currency = "ETB"
	}
	return Money{
		AmountMinor: amountMinor,
		Currency:    currency,
	}
}

// ZeroMoney returns a Money instance with 0 amount for the given currency.
func ZeroMoney(currency string) Money {
	return NewMoney(0, currency)
}

// IsZero returns true if the money amount is zero.
func (m Money) IsZero() bool {
	return m.AmountMinor == 0
}

// IsPositive returns true if amount is greater than zero.
func (m Money) IsPositive() bool {
	return m.AmountMinor > 0
}

// IsNegative returns true if amount is less than zero.
func (m Money) IsNegative() bool {
	return m.AmountMinor < 0
}

// Add sums two money amounts of the same currency.
func (m Money) Add(other Money) (Money, error) {
	if m.Currency != other.Currency {
		return Money{}, fmt.Errorf("currency mismatch: cannot add %s to %s", other.Currency, m.Currency)
	}
	return Money{
		AmountMinor: m.AmountMinor + other.AmountMinor,
		Currency:    m.Currency,
	}, nil
}

// Sub subtracts other from m of the same currency.
func (m Money) Sub(other Money) (Money, error) {
	if m.Currency != other.Currency {
		return Money{}, fmt.Errorf("currency mismatch: cannot subtract %s from %s", other.Currency, m.Currency)
	}
	return Money{
		AmountMinor: m.AmountMinor - other.AmountMinor,
		Currency:    m.Currency,
	}, nil
}

// Multiply multiplies the money amount by a factor (integer basis).
func (m Money) Multiply(factor int64) Money {
	return Money{
		AmountMinor: m.AmountMinor * factor,
		Currency:    m.Currency,
	}
}

// Percentage calculates basis points percentage (e.g. 1500 = 15.00%).
func (m Money) Percentage(basisPoints int64) Money {
	return Money{
		AmountMinor: (m.AmountMinor * basisPoints) / 10000,
		Currency:    m.Currency,
	}
}

// String returns a human-readable representation, e.g. "3500.00 ETB".
func (m Money) String() string {
	major := m.AmountMinor / 100
	minor := m.AmountMinor % 100
	if minor < 0 {
		minor = -minor
	}
	return fmt.Sprintf("%d.%02d %s", major, minor, m.Currency)
}

// Value implements the database/sql/driver.Valuer interface for database storage.
func (m Money) Value() (driver.Value, error) {
	return json.Marshal(m)
}

// Scan implements the database/sql.Scanner interface for database retrieval.
func (m *Money) Scan(value any) error {
	if value == nil {
		*m = ZeroMoney("ETB")
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("scan money: expected []byte, got %T", value)
	}
	return json.Unmarshal(bytes, m)
}
