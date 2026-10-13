package canonical_test

import (
	"bytes"
	"testing"

	"github.com/MemoryGet/memorys/pkg/canonical"
)

func TestExactCanonicalEncoding(t *testing.T) {
	for _, test := range []struct{ input, expected string }{
		{`{"b":1.2500,"a":"中文😀\n"}`, "{\"a\":\"中文😀\\n\",\"b\":125e-2}"},
		{`[10,1e1,0.0,-0,0.001200]`, `[1e1,1e1,0,0,12e-4]`},
		{`[-1.200,2E+002,null]`, `[-12e-1,2e2,null]`},
		{`{"x":"<>&/\u2028\u2029","y":"\u0001\t\r"}`, "{\"x\":\"<>&/\u2028\u2029\",\"y\":\"\\u0001\\t\\r\"}"},
		{`{"😀":true,"\ue000":false}`, "{\"\ue000\":false,\"😀\":true}"},
	} {
		actual, err := canonical.JSON([]byte(test.input))
		if err != nil || string(actual) != test.expected {
			t.Fatalf("canonical bytes mismatch: %s, %v", actual, err)
		}
		again, err := canonical.JSON(actual)
		if err != nil || !bytes.Equal(actual, again) {
			t.Fatal("canonical encoding is not idempotent")
		}
	}
}

func TestAmbiguousAndInvalidInputsAreRejected(t *testing.T) {
	for _, input := range []string{`{"x":1,"x":2}`, `{"x":1,"\u0078":2}`, `{"x":{"a":1,"a":2}}`, `{} {}`, `NaN`,
		`"\ud800"`, `"\udc00"`, `"\ud800\u0041"`, `[1e10001]`, `[1e-10001]`, `{"x":`} {
		if _, err := canonical.JSON([]byte(input)); err == nil {
			t.Fatalf("accepted invalid JSON: %s", input)
		}
	}
	if _, err := canonical.JSON([]byte{34, 255, 34}); err == nil {
		t.Fatal("accepted invalid UTF-8")
	}
}
