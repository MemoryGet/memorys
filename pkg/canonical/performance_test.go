package canonical

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func canonicalWorkload() []byte {
	rows := make([]any, 12)
	for i := range rows {
		rows[i] = map[string]any{"ID": "generation-synthetic", "Count": uint64(18446744073709551615),
			"Profile": map[string]any{"Digest": strings.Repeat("a", 64), "Channels": []string{"keyword", "vector"}},
			"Text":    "星帆恢复手册😀\n" + strings.Repeat(" recovery verified source", 18), "Enabled": true, "Missing": nil}
	}
	raw, _ := json.Marshal(map[string]any{"format": "akos.prepared-artifact/1", "artifact": rows})
	return raw
}

// This deterministic allocation gate covers the repeated artifact/index digest
// pattern observed in the native 500-document CPU profile. Wall time is measured
// by the benchmark and real HTTP matrix, not asserted by a timing-sensitive test.
func TestCanonicalAllocationBudget(t *testing.T) {
	input := canonicalWorkload()
	want, err := legacyJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	check := func(run func([]byte) ([]byte, error)) {
		got, err := run(input)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("canonical bytes changed", err)
		}
	}
	before := testing.AllocsPerRun(10, func() { check(legacyJSON) })
	after := testing.AllocsPerRun(10, func() { check(JSON) })
	t.Logf("allocations per canonical workload: legacy=%.0f current=%.0f", before, after)
	if after > before*0.6 {
		t.Fatalf("canonical allocation budget exceeded: %.0f > 60%% of %.0f", after, before)
	}
}

// Native reads canonicalize many small nested artifacts. Sorting source spans
// must avoid allocating a separate encoded buffer for every field/value.
func TestCanonicalSpanAllocationBudget(t *testing.T) {
	input := canonicalWorkload()
	want, err := legacyJSON(input)
	if err != nil {
		t.Fatal(err)
	}
	allocations := testing.AllocsPerRun(10, func() {
		got, err := JSON(input)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatal("canonical bytes changed", err)
		}
	})
	t.Logf("canonical span allocations: %.0f", allocations)
	if allocations > 160 {
		t.Fatalf("canonical span allocation budget exceeded: %.0f > 160", allocations)
	}
}

func TestCanonicalSourceSpans(t *testing.T) {
	wide := make(map[string]any)
	for n := 0; n < 65; n++ {
		wide[fmt.Sprintf("key-%03d", 64-n)] = []any{map[string]any{"z": "brace } ] and quote \\\"", "a": n}, "中文😀"}
	}
	raw, _ := json.Marshal(wide)
	inputs := [][]byte{raw,
		[]byte(`{"z":[{"q":"[\\\"}","a":2}],"a":{"中":true,"😀":null}}`),
		[]byte(`{"z":{"b":1},"x":0,"\u0078":[2]}`),
		[]byte(strings.Repeat(`{"z":[`, 32) + `0` + strings.Repeat(`]}`, 32)),
		[]byte(strings.Repeat(`{"z":[`, 33) + `0` + strings.Repeat(`]}`, 33)),
	}
	for _, input := range inputs {
		want, oldErr := legacyJSON(input)
		got, err := JSON(input)
		if (oldErr == nil) != (err == nil) || oldErr == nil && !bytes.Equal(got, want) {
			t.Fatalf("source span mismatch: %v / %v", oldErr, err)
		}
		if err == nil {
			for i := range input {
				input[i] = ' '
			}
			if !bytes.Equal(got, want) {
				t.Fatal("output aliases source input")
			}
		}
	}
}

func BenchmarkCanonicalDigestWorkload(b *testing.B) {
	input := canonicalWorkload()
	for _, impl := range []struct {
		name string
		run  func([]byte) ([]byte, error)
	}{{"legacy", legacyJSON}, {"current", JSON}} {
		b.Run(impl.name, func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(len(input)))
			for b.Loop() {
				if _, err := impl.run(input); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func FuzzCanonicalCompatibility(f *testing.F) {
	for _, input := range [][]byte{canonicalWorkload(), []byte(`{"b":1.2500,"a":"中文😀\n"}`),
		[]byte(`{"x":1,"\u0078":2}`), []byte(`"\ud800"`), []byte(`[0,-0,1e10001]`),
		[]byte(strings.Repeat("[", 64) + "0" + strings.Repeat("]", 64)),
		[]byte(strings.Repeat("[", 65) + "0" + strings.Repeat("]", 65))} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input []byte) {
		want, oldErr := legacyJSON(input)
		got, newErr := JSON(input)
		if (oldErr == nil) != (newErr == nil) || oldErr == nil && !bytes.Equal(got, want) {
			t.Fatalf("canonical compatibility mismatch: legacy=%q/%v current=%q/%v", want, oldErr, got, newErr)
		}
	})
}
