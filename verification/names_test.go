package verification_test

import (
	"testing"

	"github.com/kennankole/drop-kyc-sdk/verification"
)

func TestNamesMatch(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"wanjiru  njeri, Mary", "MARY WANJIRU NJERI", true},
		{"Mary Njeri", "MARY WANJIRU NJERI", false},
		{"O'Brien", "o brien", true},
		{"", "", false},
	}

	for _, c := range cases {
		if got := verification.NamesMatch(c.a, c.b); got != c.want {
			t.Errorf("NamesMatch(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestNormalizeID(t *testing.T) {
	if got := verification.NormalizeID(" a12 345-678z "); got != "A12345678Z" {
		t.Fatalf("got %q", got)
	}
}

func TestBusinessNamesMatch(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"Eden Bites Ltd", "EDEN BITES LIMITED", true},
		{"The Eden Bites Co.", "eden bites", true},
		{"Eden Bites", "Eden Treats Limited", false},
		{"Limited", "Ltd", false},
	}

	for _, c := range cases {
		if got := verification.BusinessNamesMatch(c.a, c.b); got != c.want {
			t.Errorf("BusinessNamesMatch(%q, %q) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
