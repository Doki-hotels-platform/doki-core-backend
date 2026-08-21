package types

import (
	"testing"
)

func TestMoney_Add(t *testing.T) {
	m1 := NewMoney(150000, "ETB") // 1,500.00 ETB
	m2 := NewMoney(50000, "ETB")  // 500.00 ETB

	sum, err := m1.Add(m2)
	if err != nil {
		t.Fatalf("unexpected error adding money: %v", err)
	}

	if sum.AmountMinor != 200000 {
		t.Errorf("expected 200000, got %d", sum.AmountMinor)
	}

	m3 := NewMoney(100, "USD")
	if _, err := m1.Add(m3); err == nil {
		t.Errorf("expected currency mismatch error, got nil")
	}
}

func TestMoney_Sub(t *testing.T) {
	m1 := NewMoney(150000, "ETB")
	m2 := NewMoney(50000, "ETB")

	diff, err := m1.Sub(m2)
	if err != nil {
		t.Fatalf("unexpected error subtracting money: %v", err)
	}

	if diff.AmountMinor != 100000 {
		t.Errorf("expected 100000, got %d", diff.AmountMinor)
	}
}

func TestMoney_Percentage(t *testing.T) {
	// 1000.00 ETB with 15% commission (1500 basis points) = 150.00 ETB
	total := NewMoney(100000, "ETB")
	commission := total.Percentage(1500)

	if commission.AmountMinor != 15000 {
		t.Errorf("expected 15000 (150.00 ETB), got %d", commission.AmountMinor)
	}
}

func TestMoney_Formatting(t *testing.T) {
	m := NewMoney(350050, "ETB")
	expected := "3500.50 ETB"
	if str := m.String(); str != expected {
		t.Errorf("expected %s, got %s", expected, str)
	}
}
