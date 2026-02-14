package probe

import (
	"fmt"
	"strings"
	"testing"
)

func TestGenerateIdentityCases(t *testing.T) {
	// Generate with two different seeds — should produce different questions
	cases1 := generateIdentityCases(5, 12345)
	cases2 := generateIdentityCases(5, 99999)

	if len(cases1) != 15 {
		t.Fatalf("expected 15 cases, got %d", len(cases1))
	}
	if len(cases2) != 15 {
		t.Fatalf("expected 15 cases, got %d", len(cases2))
	}

	// Check tier distribution: 5 easy, 5 medium, 5 hard
	tierCount := map[string]int{}
	for _, c := range cases1 {
		tierCount[c.Tier]++
	}
	for _, tier := range []string{"easy", "medium", "hard"} {
		if tierCount[tier] != 5 {
			t.Errorf("expected 5 %s cases, got %d", tier, tierCount[tier])
		}
	}

	// Check IDs are unique
	ids := map[string]bool{}
	for _, c := range cases1 {
		if ids[c.ID] {
			t.Errorf("duplicate ID: %s", c.ID)
		}
		ids[c.ID] = true
	}

	// Check all cases have non-empty fields
	for _, c := range cases1 {
		if c.Question == "" {
			t.Errorf("case %s has empty question", c.ID)
		}
		if c.Expected == "" {
			t.Errorf("case %s has empty expected", c.ID)
		}
	}

	// Check different seeds produce different questions
	same := 0
	for i := range cases1 {
		if cases1[i].Question == cases2[i].Question {
			same++
		}
	}
	if same > 3 {
		t.Errorf("different seeds produced too many identical questions: %d/15", same)
	}

	// Print sample for visual inspection
	for _, c := range cases1 {
		t.Logf("[%s] %s: Q=%s A=%s", c.Tier, c.ID, truncate(c.Question, 80), c.Expected)
	}
}

func TestGenerateIdentityCasesAnswerValidity(t *testing.T) {
	// Run multiple seeds and verify answers are parseable by equivalentAnswer
	for seed := int64(1); seed <= 5; seed++ {
		cases := generateIdentityCases(5, seed*7777)
		for _, c := range cases {
			// Expected should not be empty
			if strings.TrimSpace(c.Expected) == "" {
				t.Errorf("seed=%d case=%s: empty expected answer", seed, c.ID)
			}
			// Expected should match itself
			ok, method := equivalentAnswer(c.Expected, c.Expected)
			if !ok {
				t.Errorf("seed=%d case=%s: expected answer doesn't match itself: %q (method=%s)", seed, c.ID, c.Expected, method)
			}
		}
	}
}

func TestParseClaimedTier(t *testing.T) {
	tests := []struct {
		model string
		want  string
	}{
		{"claude-3-opus-20240229", "opus"},
		{"claude-3-5-sonnet-20241022", "sonnet"},
		{"claude-3-haiku-20240307", "haiku"},
		{"anthropic.claude-3-opus-20240229-v1:0", "opus"},
		{"anthropic.claude-3-sonnet-20240229-v1:0", "sonnet"},
		{"claude-3-5-haiku-20241022", "haiku"},
		{"gpt-4o", "unknown"},
		{"", "unknown"},
	}
	for _, tt := range tests {
		got := parseClaimedTier(tt.model)
		if got != tt.want {
			t.Errorf("parseClaimedTier(%q) = %q, want %q", tt.model, got, tt.want)
		}
	}
}

func TestEstimateTier(t *testing.T) {
	tests := []struct {
		name     string
		easy     float64
		med      float64
		hard     float64
		outLen   int
		wantTier string
	}{
		{"opus-like", 1.0, 0.8, 0.6, 300, "opus"},
		{"sonnet-like", 1.0, 0.6, 0.2, 150, "sonnet"},
		{"haiku-like", 0.8, 0.2, 0.0, 60, "haiku"},
	}
	for _, tt := range tests {
		tier, conf, _ := estimateTier(tt.easy, tt.med, tt.hard, tt.outLen)
		if tier != tt.wantTier {
			t.Errorf("%s: estimateTier = %q, want %q (conf=%.2f)", tt.name, tier, tt.wantTier, conf)
		}
	}
}

func TestBuildIdentityPrompt(t *testing.T) {
	cases := generateIdentityCases(2, 42)
	prompt := buildIdentityPrompt(cases)
	if !strings.Contains(prompt, "JSON") {
		t.Error("prompt should mention JSON")
	}
	// Should contain all case IDs
	for _, c := range cases {
		if !strings.Contains(prompt, c.ID+":") {
			t.Errorf("prompt missing case ID %s", c.ID)
		}
	}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func TestHelperFunctions(t *testing.T) {
	// gcd
	if g := gcd(12, 8); g != 4 {
		t.Errorf("gcd(12,8) = %d, want 4", g)
	}
	// factorial
	if f := factorial(5); f != 120 {
		t.Errorf("factorial(5) = %d, want 120", f)
	}
	// comb
	if c := comb(10, 3); c != 120 {
		t.Errorf("comb(10,3) = %d, want 120", c)
	}
	// isPrime
	primes := []int{2, 3, 5, 7, 11, 13}
	for _, p := range primes {
		if !isPrime(p) {
			t.Errorf("isPrime(%d) should be true", p)
		}
	}
	nonPrimes := []int{0, 1, 4, 6, 9, 15}
	for _, n := range nonPrimes {
		if isPrime(n) {
			t.Errorf("isPrime(%d) should be false", n)
		}
	}
	// eulerTotient
	if et := eulerTotient(12); et != 4 {
		t.Errorf("eulerTotient(12) = %d, want 4", et)
	}
	// modPow
	if mp := modPow(2, 10, 1000); mp != 24 {
		t.Errorf("modPow(2,10,1000) = %d, want 24", mp)
	}
	// det3x3
	m := [3][3]int{{1, 2, 3}, {4, 5, 6}, {7, 8, 9}}
	if d := det3x3(m); d != 0 {
		t.Errorf("det3x3 singular = %d, want 0", d)
	}
	m2 := [3][3]int{{1, 0, 0}, {0, 2, 0}, {0, 0, 3}}
	if d := det3x3(m2); d != 6 {
		t.Errorf("det3x3 diag = %d, want 6", d)
	}
	// formatFrac
	if f := formatFrac(6, 4); f != "3/2" {
		t.Errorf("formatFrac(6,4) = %q, want 3/2", f)
	}
	if f := formatFrac(8, 2); f != "4" {
		t.Errorf("formatFrac(8,2) = %q, want 4", f)
	}
}

func TestDeterminismSameSeed(t *testing.T) {
	cases1 := generateIdentityCases(5, 42)
	cases2 := generateIdentityCases(5, 42)
	for i := range cases1 {
		if cases1[i].Question != cases2[i].Question || cases1[i].Expected != cases2[i].Expected {
			t.Errorf("same seed produced different case at index %d", i)
		}
	}
}

func Example_generateIdentityCases() {
	cases := generateIdentityCases(2, 42)
	for _, c := range cases {
		fmt.Printf("[%s] %s\n", c.Tier, c.ID)
	}
	// Output will vary but should have 6 cases (2 per tier)
}
