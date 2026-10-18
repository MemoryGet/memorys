package postgres

import "testing"

func TestFormatEventCursorIsFixedWidthAndMonotonic(t *testing.T) {
	for _, test := range []struct {
		sequence int64
		want     string
	}{
		{0, ""},
		{1, "00000000000000000001"},
		{42, "00000000000000000042"},
		{9223372036854775807, "09223372036854775807"},
	} {
		if got := formatEventCursor(test.sequence); got != test.want {
			t.Fatalf("formatEventCursor(%d)=%q, want %q", test.sequence, got, test.want)
		}
	}
	if formatEventCursor(1) >= formatEventCursor(2) || formatEventCursor(2) >= formatEventCursor(100) {
		t.Fatal("event cursors are not lexically monotonic")
	}
}
