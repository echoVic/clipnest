package main

import (
	"strings"
	"testing"
)

// Tokens are assembled at run time so the source holds no complete token
// literal for secret scanners to flag.
func assemble(parts ...string) string { return strings.Join(parts, "") }

var (
	testAWSKey    = assemble("AKIA", "IOSFODNN7", "EXAMPLE")
	testGitHub    = assemble("gh", "p_", "aB3dE5fG7hJ9kL1mN3pQ", "5rS7tU9vW1xY3zA5")
	testGitHubPAT = assemble("github", "_pat_", "11ABCDEFG0", strings.Repeat("aZ9_", 18), "wxyz")
	testGitLab    = assemble("gl", "pat-", "xY12ab34CD56ef78GH90")
	testSlack     = assemble("xo", "xb-", "1234567890-", "abcdefghij")
	testAnthropic = assemble("sk", "-ant-", "api03-", strings.Repeat("Ab1-", 10), "Zz9q")
	testStripe    = assemble("sk", "_live_", "4eC39HqLyjWD", "arjtT1zdp7dc")
	testOpenAI    = assemble("sk", "-proj-", strings.Repeat("Q7w", 12), "Lm2n")
	testGoogle    = assemble("AI", "za", "SyD1e2f3g4h5i6j7k8l9", "m0n1o2p3q4r5s6t")
	testJWT       = assemble("ey", "JhbGciOiJIUzI1NiJ9", ".", "eyJzdWIiOiIxMjM0NTY3ODkwIn0", ".", "dozjgNryP4J3jVmNHl0w5N_XgL0n3I9P")
	testKeyBegin  = assemble("-----BEGIN ", "OPENSSH PRIVATE KEY-----")
	testKeyEnd    = assemble("-----END ", "OPENSSH PRIVATE KEY-----")
)

func TestMaskSecrets(t *testing.T) {
	for _, tc := range []struct {
		name, text, label, masked string
	}{
		{"aws", testAWSKey, "AWS 访问密钥", "AKIA••••MPLE"},
		{"github", testGitHub, "GitHub 令牌", "ghp_••••" + testGitHub[len(testGitHub)-4:]},
		{"github fine-grained", testGitHubPAT, "GitHub 令牌", "github_pat_••••wxyz"},
		{"gitlab", testGitLab, "GitLab 令牌", "glpat-••••GH90"},
		{"slack", testSlack, "Slack 令牌", "xoxb-••••ghij"},
		{"anthropic before generic sk-", testAnthropic, "Anthropic 密钥", "sk-ant-••••Zz9q"},
		{"stripe", testStripe, "Stripe 密钥", "sk_live_••••p7dc"},
		{"openai project key", testOpenAI, "API 密钥", "sk-proj-••••Lm2n"},
		{"google", testGoogle, "Google API 密钥", "AIza••••" + testGoogle[len(testGoogle)-4:]},
		{"jwt", testJWT, "JWT 令牌", "eyJ••••" + testJWT[len(testJWT)-4:]},
		{"private key", testKeyBegin + "\nb3BlbnNzaC1rZXktdjEAAAAA\nAAAABG5vbmUAAAAEbm9uZQ==\n" + testKeyEnd + "\n",
			"私钥", testKeyBegin + "\n••••\n" + testKeyEnd + "\n"},
		{"truncated private key", "key:\n" + testKeyBegin + "\nb3BlbnNzaC1rZXktdjEAAAAA", "私钥", "key:\n" + testKeyBegin + "\n••••"},
		{"visa card", "4111 1111 1111 1111", "银行卡号", "•••• 1111"},
		{"mastercard with dashes", " 5555-5555-5555-4444\n", "银行卡号", " •••• 4444\n"},
		{"amex", "378282246310005", "银行卡号", "•••• 0005"},
		{"unionpay", "6212345678901232", "银行卡号", "•••• 1232"},
		{"env file keeps other lines", "DB_HOST=localhost\nGITHUB_TOKEN=" + testGitHub + "\nDEBUG=1",
			"GitHub 令牌", "DB_HOST=localhost\nGITHUB_TOKEN=ghp_••••" + testGitHub[len(testGitHub)-4:] + "\nDEBUG=1"},
		{"label of the first secret", "a " + testSlack + " b " + testAWSKey, "Slack 令牌", "a xoxb-••••ghij b AKIA••••MPLE"},
		{"bearer header", "Authorization: Bearer " + testJWT, "JWT 令牌", "Authorization: Bearer eyJ••••" + testJWT[len(testJWT)-4:]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			masked, label := maskSecrets(tc.text)
			if label != tc.label || masked != tc.masked {
				t.Fatalf("maskSecrets(%q) = %q, %q; want %q, %q", tc.text, masked, label, tc.masked, tc.label)
			}
		})
	}
}

func TestMaskSecretsIgnoresLookalikes(t *testing.T) {
	for _, text := range []string{
		"",
		"3c9a411f2b7d4e1c8a6b5d3e2f1a0b9c8d7e6f5a",        // git hash
		"sk-button-primary-large-size-variant-default",    // kebab-case, no digit
		"task-abcdefghijklmnopqrstuvwxyz0123456789abcdef", // "sk-" inside a word
		"sk-1234567",                                 // too short
		"订单号 4111111111111111 已发货",                   // a number inside text
		"4111 1111 1111 1112",                        // fails Luhn
		"1234567812345670",                           // passes Luhn, no card network
		"4111  1111 1111 1111",                       // irregular grouping
		"AKIA is the prefix of an AWS access key ID", // prefix only
		"eyJhbGciOiJIUzI1NiJ9 is a JWT header",       // one segment
		assemble("-----BEGIN ", "PUBLIC KEY-----\nMIIBIjAN\n-----END ", "PUBLIC KEY-----"), // public key
		"https://example.com/docs/ghp_tokens",                                              // too short after ghp_
	} {
		if masked, label := maskSecrets(text); label != "" || masked != text {
			t.Errorf("maskSecrets(%q) = %q, %q; want unchanged", text, masked, label)
		}
	}
}

func TestEntryMask(t *testing.T) {
	text := "export AWS_ACCESS_KEY_ID=" + testAWSKey + "\nexport REGION=us-east-1"
	e, err := newEntry(text, nil, "Terminal")
	if err != nil {
		t.Fatal(err)
	}
	id := e.ID
	if !e.mask() {
		t.Fatal("mask() = false, want true")
	}
	if e.Secret != "AWS 访问密钥" || e.ID != id || e.Text != text {
		t.Fatalf("secret %q id changed %v text changed %v", e.Secret, e.ID != id, e.Text != text)
	}
	for name, field := range map[string]string{"title": e.Title, "excerpt": e.Excerpt, "masked": e.Masked, "search": e.searchText} {
		if strings.Contains(field, testAWSKey) || strings.Contains(strings.ToLower(field), strings.ToLower(testAWSKey)) {
			t.Errorf("%s keeps the key: %q", name, field)
		}
	}
	if e.Title != "export AWS_ACCESS_KEY_ID=AKIA••••MPLE" || !strings.Contains(e.searchText, "region=us-east-1") {
		t.Fatalf("title %q search %q", e.Title, e.searchText)
	}
	if s := e.summary(); s.Masked != "" || s.Secret == "" {
		t.Fatalf("summary keeps masked text or drops the type: %+v", s)
	}

	plain, _ := newEntry("nothing to hide", nil, "Notes")
	before := plain
	if plain.mask() || plain.Secret != "" || plain.Title != before.Title || plain.searchText != before.searchText {
		t.Fatalf("plain text changed: %+v", plain)
	}
	img := Entry{Kind: "image", Text: testAWSKey}
	if img.mask() {
		t.Fatal("images are not masked")
	}
}
