package main

import (
	"runtime"
	"testing"
)

func TestWebParallelismRespectsRuntimeAndOperatorLimits(t *testing.T) {
	previous := runtime.GOMAXPROCS(0)
	defer runtime.GOMAXPROCS(previous)
	for _, test := range []struct {
		name          string
		setting       string
		initial, want int
	}{
		{"default above admission", "", 8, 4},
		{"smaller runtime limit", "", 2, 2},
		{"explicit operator setting", "6", 6, 6},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("GOMAXPROCS", test.setting)
			runtime.GOMAXPROCS(test.initial)
			configureWebParallelism()
			if got := runtime.GOMAXPROCS(0); got != test.want {
				t.Fatalf("configured parallelism = %d, want %d", got, test.want)
			}
		})
	}
}
