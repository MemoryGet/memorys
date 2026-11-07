package main

import "testing"

func TestHighQoSAppliesOnlyToWebCommand(t *testing.T) {
	for _, test := range []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"web", "--address", "127.0.0.1:8080"}, true},
		{[]string{"serve"}, false},
		{[]string{"init"}, false},
		{[]string{"semantic-search", "question"}, false},
	} {
		if got := webCommand(test.args); got != test.want {
			t.Errorf("webCommand(%q) = %v, want %v", test.args, got, test.want)
		}
	}
}
