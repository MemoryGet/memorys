package modulewire

import (
	"math"
	"testing"
	"time"
)

func TestProbeTimeNSRangeAndTimezone(t *testing.T) {
	for _, v := range []time.Time{time.Time{}, time.Unix(0, 0), time.Unix(0, -1), time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)} {
		if positiveNS(v) {
			t.Fatal("unrepresentable positive int64 time accepted", v.Year())
		}
	}
	for _, v := range []time.Time{time.Unix(0, 1), time.Unix(0, math.MaxInt64), time.Unix(1900000000, 123).In(time.FixedZone("offset", 8*3600))} {
		if !positiveNS(v) {
			t.Fatal("representable time rejected", v)
		}
	}
}
