package model

import "testing"

func TestEscapeLikeLiteral(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"opus", "opus"},      // no special chars
		{"my_key", "my!_key"}, // _ is a LIKE wildcard
		{"50%", "50!%"},       // % is a LIKE wildcard
		{"a!b", "a!!b"},       // the escape char itself
		{"x_%!y", "x!_!%!!y"}, // escape ! first, then % and _
		{"gpt-4o", "gpt-4o"},  // hyphen is literal
	}
	for _, c := range cases {
		if got := escapeLikeLiteral(c.in); got != c.want {
			t.Errorf("escapeLikeLiteral(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
