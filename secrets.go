package main

import (
	"database/sql"
	"regexp"
	"slices"
	"strings"
)

// A secret is masked where it appears; the rest of the text stays readable.
// Only formats with a distinctive prefix or checksum are recognized, so
// ordinary identifiers, hashes and numbers are left alone.
type secretSpan struct {
	start, end int
	label      string
	masked     string
}

type tokenRule struct {
	label string
	re    *regexp.Regexp // group 1 is the visible prefix
	digit bool           // the token body must contain a digit
}

// Earlier rules win when two start at the same place: sk-ant- before sk-.
var tokenRules = []tokenRule{
	{label: "AWS 访问密钥", re: regexp.MustCompile(`\b(AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{label: "GitHub 令牌", re: regexp.MustCompile(`\b(gh[pousr]_)[A-Za-z0-9]{36,255}\b`)},
	{label: "GitHub 令牌", re: regexp.MustCompile(`\b(github_pat_)[A-Za-z0-9_]{22,255}`)},
	{label: "GitLab 令牌", re: regexp.MustCompile(`\b(glpat-)[A-Za-z0-9_-]{20,}`)},
	{label: "Slack 令牌", re: regexp.MustCompile(`\b(xox[abposr]-)[A-Za-z0-9-]{10,}`)},
	{label: "Anthropic 密钥", re: regexp.MustCompile(`\b(sk-ant-)[A-Za-z0-9_-]{20,}`)},
	{label: "Stripe 密钥", re: regexp.MustCompile(`\b([rs]k_(?:live|test)_)[A-Za-z0-9]{16,}`)},
	{label: "API 密钥", re: regexp.MustCompile(`\b(sk-(?:proj-)?)[A-Za-z0-9_-]{32,}`), digit: true},
	{label: "Google API 密钥", re: regexp.MustCompile(`\b(AIza)[0-9A-Za-z_-]{35}`)},
	{label: "JWT 令牌", re: regexp.MustCompile(`\b(eyJ)[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)},
}

var privateKeyBegin = regexp.MustCompile(`-----BEGIN ((?:[A-Z0-9]+ )*)PRIVATE KEY( BLOCK)?-----`)

// A card number is recognized only when it is the whole copy: digits in
// groups separated by single spaces or hyphens.
var cardNumber = regexp.MustCompile(`^\d(?:[ -]?\d){12,18}$`)

const maskDots = "••••"

func findSecrets(text string) []secretSpan {
	var spans []secretSpan
	for _, m := range privateKeyBegin.FindAllStringSubmatchIndex(text, -1) {
		begin, block := text[m[0]:m[1]], ""
		if m[4] >= 0 {
			block = text[m[4]:m[5]]
		}
		end := "-----END " + text[m[2]:m[3]] + "PRIVATE KEY" + block + "-----"
		span := secretSpan{start: m[0], end: len(text), label: "私钥", masked: begin + "\n" + maskDots}
		if i := strings.Index(text[m[1]:], end); i >= 0 {
			span.end = m[1] + i + len(end)
			span.masked += "\n" + end
		}
		spans = append(spans, span)
	}
	for _, rule := range tokenRules {
		for _, m := range rule.re.FindAllStringSubmatchIndex(text, -1) {
			prefix, body := text[m[2]:m[3]], text[m[3]:m[1]]
			if rule.digit && !strings.ContainsAny(body, "0123456789") {
				continue
			}
			spans = append(spans, secretSpan{start: m[0], end: m[1], label: rule.label, masked: prefix + maskDots + body[len(body)-4:]})
		}
	}
	if trimmed := strings.TrimSpace(text); cardNumber.MatchString(trimmed) {
		digits := strings.Map(func(r rune) rune {
			if r == ' ' || r == '-' {
				return -1
			}
			return r
		}, trimmed)
		if cardNetwork(digits) && luhn(digits) {
			start := strings.Index(text, trimmed)
			spans = append(spans, secretSpan{start: start, end: start + len(trimmed), label: "银行卡号", masked: maskDots + " " + digits[len(digits)-4:]})
		}
	}
	// Keep the earliest span; on a tie the one found first (its rule is listed first).
	slices.SortStableFunc(spans, func(a, b secretSpan) int { return a.start - b.start })
	kept := spans[:0]
	for _, s := range spans {
		if len(kept) == 0 || s.start >= kept[len(kept)-1].end {
			kept = append(kept, s)
		}
	}
	return kept
}

// maskSecrets returns text with each secret masked and the type of the first
// one, or text unchanged and "" when it holds none.
func maskSecrets(text string) (string, string) {
	spans := findSecrets(text)
	if len(spans) == 0 {
		return text, ""
	}
	var b strings.Builder
	last := 0
	for _, s := range spans {
		b.WriteString(text[last:s.start])
		b.WriteString(s.masked)
		last = s.end
	}
	b.WriteString(text[last:])
	return b.String(), spans[0].label
}

// Visa, Mastercard, American Express, Discover, JCB, UnionPay and Diners Club.
func cardNetwork(digits string) bool {
	if len(digits) < 13 || len(digits) > 19 {
		return false
	}
	prefix := func(n int) int {
		v := 0
		for _, c := range digits[:n] {
			v = v*10 + int(c-'0')
		}
		return v
	}
	p2, p3, p4 := prefix(2), prefix(3), prefix(4)
	switch {
	case digits[0] == '4':
		return true
	case p2 >= 51 && p2 <= 55, p4 >= 2221 && p4 <= 2720:
		return true
	case p2 == 34 || p2 == 37:
		return true
	case p4 == 6011, p3 >= 644 && p3 <= 649, p2 == 65:
		return true
	case p4 >= 3528 && p4 <= 3589:
		return true
	case p2 == 62:
		return true
	case p3 >= 300 && p3 <= 305, p2 == 36, p2 == 38, p2 == 39:
		return true
	}
	return false
}

func luhn(digits string) bool {
	sum := 0
	for i := range len(digits) {
		d := int(digits[len(digits)-1-i] - '0')
		if i%2 == 1 {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
	}
	return sum%10 == 0
}

// mask rewrites a text entry's title, excerpt and search text from its masked
// form and records the type of secret. Text and ID keep the original, which is
// what gets pasted. It reports whether the entry holds a secret.
func (e *Entry) mask() bool {
	if e.Kind == "image" {
		return false
	}
	masked, label := maskSecrets(e.Text)
	if label == "" {
		return false
	}
	e.Secret, e.Masked = label, masked
	e.Title, e.Excerpt = textTitles(masked)
	e.searchText = prepareSearch(*e)
	return true
}

// remask restores the mask of an entry recorded as a secret. When the rules no
// longer recognize its text, all of it is hidden rather than shown.
func (e *Entry) remask(secret string) {
	if secret == "" || e.Kind == "image" {
		return
	}
	if !e.mask() {
		e.Masked = maskDots
		e.Title, e.Excerpt = textTitles(maskDots)
	}
	e.Secret = secret
	e.searchText = prepareSearch(*e)
}

// The history column that stores a masked entry's secret type.
const secretColumn = "secret TEXT NOT NULL DEFAULT ''"

func (s *sqliteStore) upgradeSecret() error {
	return s.transaction(func(tx *sql.Tx) error {
		_, err := tx.Exec("ALTER TABLE history ADD COLUMN " + secretColumn + "; PRAGMA user_version=5")
		return err
	})
}
