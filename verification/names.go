package verification

import (
	"slices"
	"strings"
	"unicode"
)

// NamesMatch compares two personal or business names, ignoring case,
// punctuation, extra spaces and word order ("Njeri, Mary" matches
// "MARY NJERI"). It does not forgive a missing or different word: whether
// "Mary Njeri" is "Mary Wanjiru Njeri" is a reviewer's call, so providers
// report that as inconclusive rather than pass.
func NamesMatch(a, b string) bool {
	ta, tb := nameTokens(a), nameTokens(b)

	return len(ta) > 0 && slices.Equal(ta, tb)
}

// legalForms are words that state a business's legal form rather than its
// name, which registries and applicants include inconsistently.
var legalForms = map[string]bool{
	"LIMITED": true, "LTD": true, "PLC": true, "LLP": true, "LLC": true, "INC": true,
	"COMPANY": true, "CO": true, "THE": true,
}

// BusinessNamesMatch compares business names like NamesMatch, also ignoring
// words for the legal form ("Eden Bites Ltd" matches "EDEN BITES LIMITED").
func BusinessNamesMatch(a, b string) bool {
	ta, tb := withoutLegalForms(nameTokens(a)), withoutLegalForms(nameTokens(b))

	return len(ta) > 0 && slices.Equal(ta, tb)
}

func withoutLegalForms(tokens []string) []string {
	out := tokens[:0:0]

	for _, t := range tokens {
		if !legalForms[t] {
			out = append(out, t)
		}
	}

	return out
}

func nameTokens(name string) []string {
	tokens := strings.FieldsFunc(strings.ToUpper(name), func(r rune) bool {
		return !unicode.IsLetter(r)
	})
	slices.Sort(tokens)

	return tokens
}

// NormalizeID canonicalizes an identity, tax or license number for
// comparison: spaces and hyphens are dropped and letters upper-cased.
func NormalizeID(raw string) string {
	var b strings.Builder

	for _, r := range strings.TrimSpace(raw) {
		if unicode.IsSpace(r) || r == '-' {
			continue
		}

		b.WriteRune(unicode.ToUpper(r))
	}

	return b.String()
}
