package probe

import "testing"

func TestEquivalentAnswer(t *testing.T) {
	tests := []struct {
		name     string
		expected string
		got      string
		wantOK   bool
	}{
		{name: "exact", expected: "metabolic acidosis", got: "metabolicacidosis", wantOK: true},
		{name: "numeric float/int", expected: "7200", got: "7200.0", wantOK: true},
		{name: "numeric percent", expected: "80", got: "80%", wantOK: true},
		{name: "unit time conversion", expected: "1h", got: "60min", wantOK: true},
		{name: "unit bytes conversion", expected: "1gi", got: "1024mi", wantOK: true},
		{name: "unit currency conversion", expected: "$1200", got: "1200 dollars", wantOK: true},
		{name: "date", expected: "2026-01-06", got: "2026/01/06", wantOK: true},
		{name: "choice", expected: "B", got: "option b", wantOK: true},
		{name: "bool", expected: "yes", got: "true", wantOK: true},
		{name: "text primary synonym", expected: "metabolic acidosis", got: "primary metabolic acidosis", wantOK: true},
		{name: "alternatives", expected: "a||b", got: "B", wantOK: true},
		{name: "unit mismatch", expected: "1h", got: "1kg", wantOK: false},
		{name: "mismatch", expected: "90", got: "91", wantOK: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ok, _ := equivalentAnswer(tc.expected, tc.got)
			if ok != tc.wantOK {
				t.Fatalf("equivalentAnswer(%q,%q)=%v want %v", tc.expected, tc.got, ok, tc.wantOK)
			}
		})
	}
}

func TestAnalyzeReasoningCaseSet(t *testing.T) {
	cases := []reasoningCase{
		{ID: "q1", Domain: "law", Question: "Q", Expected: "yes"},
		{ID: "q1", Domain: "law", Question: "Q", Expected: "yes"},
		{ID: "q2", Domain: "finance", Question: "Q2", Expected: "no"},
		{ID: "q3", Domain: "finance", Question: "Q3", Expected: "no"},
	}
	stats := analyzeReasoningCaseSet(cases)
	if stats.DuplicateIDCount != 1 {
		t.Fatalf("DuplicateIDCount=%d want 1", stats.DuplicateIDCount)
	}
	if stats.DuplicateQuestionCount != 1 {
		t.Fatalf("DuplicateQuestionCount=%d want 1", stats.DuplicateQuestionCount)
	}
	if stats.DuplicateExpectedCount != 2 {
		t.Fatalf("DuplicateExpectedCount=%d want 2", stats.DuplicateExpectedCount)
	}
	if stats.UniqueExpectedCount != 2 {
		t.Fatalf("UniqueExpectedCount=%d want 2", stats.UniqueExpectedCount)
	}
	if stats.ConstantGuessUpperBound <= 0.4 {
		t.Fatalf("ConstantGuessUpperBound=%f want >0.4", stats.ConstantGuessUpperBound)
	}
	if stats.MaxDomainShare <= 0.4 {
		t.Fatalf("MaxDomainShare=%f want >0.4", stats.MaxDomainShare)
	}
}
