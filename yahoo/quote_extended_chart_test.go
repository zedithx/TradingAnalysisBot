package yahoo

import "testing"

func fptr(v float64) *float64 { return &v }

func TestLastCloseInWindow(t *testing.T) {
	timestamps := []int64{10, 20, 30, 40, 50}
	closes := []*float64{fptr(1.1), fptr(1.2), nil, fptr(1.4), fptr(1.5)}

	got := lastCloseInWindow(timestamps, closes, 15, 45)
	want := 1.4
	if got != want {
		t.Fatalf("lastCloseInWindow() = %.2f, want %.2f", got, want)
	}
}

func TestLastCloseInWindow_NoData(t *testing.T) {
	timestamps := []int64{10, 20, 30}
	closes := []*float64{nil, fptr(0), fptr(3.3)}

	got := lastCloseInWindow(timestamps, closes, 31, 40)
	if got != 0 {
		t.Fatalf("lastCloseInWindow() = %.2f, want 0", got)
	}
}
