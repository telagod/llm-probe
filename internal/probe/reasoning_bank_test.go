package probe

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseReasoningBankLegacyArray(t *testing.T) {
	data := []byte(`[{"id":"case_1","domain":"finance","difficulty":"easy","question":"1+1?","expected":"2"}]`)
	cases, meta, err := parseReasoningBank(data, reasoningBankMetadata{Path: "legacy.json"})
	if err != nil {
		t.Fatalf("parseReasoningBank legacy failed: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(cases))
	}
	if meta.Format != "legacy_array" {
		t.Fatalf("expected legacy_array format, got %q", meta.Format)
	}
	if meta.Version != "legacy-array" {
		t.Fatalf("expected legacy-array version, got %q", meta.Version)
	}
}

func TestParseReasoningBankEnvelope(t *testing.T) {
	data := []byte(`{"version":"1.0","name":"test-bank","source":"unit","created_at":"2026-01-01T00:00:00Z","cases":[{"id":"case_2","domain":"law","difficulty":"medium","question":"deadline?","expected":"2026-01-02"}]}`)
	cases, meta, err := parseReasoningBank(data, reasoningBankMetadata{Path: "envelope.json"})
	if err != nil {
		t.Fatalf("parseReasoningBank envelope failed: %v", err)
	}
	if len(cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(cases))
	}
	if meta.Format != "envelope" {
		t.Fatalf("expected envelope format, got %q", meta.Format)
	}
	if meta.Name != "test-bank" {
		t.Fatalf("unexpected name: %q", meta.Name)
	}
}

func TestImportReasoningBankGSM8K(t *testing.T) {
	tmpDir := t.TempDir()
	inPath := filepath.Join(tmpDir, "gsm8k.jsonl")
	outPath := filepath.Join(tmpDir, "bank.json")
	if err := os.WriteFile(inPath, []byte("{\"question\":\"2+3?\",\"answer\":\"calc #### 5\"}\n"), 0o644); err != nil {
		t.Fatalf("write input failed: %v", err)
	}

	summary, err := ImportReasoningBank(ReasoningImportConfig{
		InputPath:  inPath,
		OutputPath: outPath,
		Format:     "gsm8k_jsonl",
		Domain:     "math_reasoning",
	})
	if err != nil {
		t.Fatalf("ImportReasoningBank failed: %v", err)
	}
	if summary.CaseCount != 1 {
		t.Fatalf("expected case_count=1, got %d", summary.CaseCount)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output failed: %v", err)
	}
	var bank reasoningBankEnvelope
	if err := json.Unmarshal(raw, &bank); err != nil {
		t.Fatalf("unmarshal output failed: %v", err)
	}
	if bank.Version != reasoningBankSchemaVersion {
		t.Fatalf("unexpected bank version: %q", bank.Version)
	}
	if len(bank.Cases) != 1 || bank.Cases[0].Expected != "5" {
		t.Fatalf("unexpected imported cases: %+v", bank.Cases)
	}
}

func TestImportReasoningBankMMLU(t *testing.T) {
	tmpDir := t.TempDir()
	inPath := filepath.Join(tmpDir, "mmlu.csv")
	outPath := filepath.Join(tmpDir, "bank.json")
	csv := "question,A,B,C,D,answer\nWhat is 2+2?,1,2,3,4,D\n"
	if err := os.WriteFile(inPath, []byte(csv), 0o644); err != nil {
		t.Fatalf("write csv failed: %v", err)
	}

	summary, err := ImportReasoningBank(ReasoningImportConfig{
		InputPath:  inPath,
		OutputPath: outPath,
		Format:     "mmlu_csv",
		Domain:     "mmlu_math",
	})
	if err != nil {
		t.Fatalf("ImportReasoningBank mmlu failed: %v", err)
	}
	if summary.CaseCount != 1 {
		t.Fatalf("expected case_count=1, got %d", summary.CaseCount)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output failed: %v", err)
	}
	var bank reasoningBankEnvelope
	if err := json.Unmarshal(raw, &bank); err != nil {
		t.Fatalf("unmarshal output failed: %v", err)
	}
	if len(bank.Cases) != 1 || bank.Cases[0].Expected != "d" {
		t.Fatalf("unexpected imported case: %+v", bank.Cases)
	}
}

func TestImportReasoningBankBBH(t *testing.T) {
	tmpDir := t.TempDir()
	inPath := filepath.Join(tmpDir, "bbh_boolean_expressions.jsonl")
	outPath := filepath.Join(tmpDir, "bank.json")
	if err := os.WriteFile(inPath, []byte("{\"input\":\"Is 2 > 1?\",\"target\":\"yes\"}\n"), 0o644); err != nil {
		t.Fatalf("write input failed: %v", err)
	}

	summary, err := ImportReasoningBank(ReasoningImportConfig{
		InputPath:  inPath,
		OutputPath: outPath,
		Format:     "bbh_jsonl",
		Domain:     "benchmark_reasoning",
	})
	if err != nil {
		t.Fatalf("ImportReasoningBank bbh failed: %v", err)
	}
	if summary.CaseCount != 1 {
		t.Fatalf("expected case_count=1, got %d", summary.CaseCount)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output failed: %v", err)
	}
	var bank reasoningBankEnvelope
	if err := json.Unmarshal(raw, &bank); err != nil {
		t.Fatalf("unmarshal output failed: %v", err)
	}
	if len(bank.Cases) != 1 || bank.Cases[0].Expected != "yes" {
		t.Fatalf("unexpected imported case: %+v", bank.Cases)
	}
}

func TestImportReasoningBankARC(t *testing.T) {
	tmpDir := t.TempDir()
	inPath := filepath.Join(tmpDir, "arc_test.jsonl")
	outPath := filepath.Join(tmpDir, "bank.json")
	row := "{\"question\":{\"stem\":\"Red planet?\",\"choices\":[{\"label\":\"A\",\"text\":\"Earth\"},{\"label\":\"B\",\"text\":\"Mars\"}]},\"answerKey\":\"B\"}\n"
	if err := os.WriteFile(inPath, []byte(row), 0o644); err != nil {
		t.Fatalf("write input failed: %v", err)
	}

	summary, err := ImportReasoningBank(ReasoningImportConfig{
		InputPath:  inPath,
		OutputPath: outPath,
		Format:     "arc_jsonl",
		Domain:     "science_qa",
	})
	if err != nil {
		t.Fatalf("ImportReasoningBank arc failed: %v", err)
	}
	if summary.CaseCount != 1 {
		t.Fatalf("expected case_count=1, got %d", summary.CaseCount)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output failed: %v", err)
	}
	var bank reasoningBankEnvelope
	if err := json.Unmarshal(raw, &bank); err != nil {
		t.Fatalf("unmarshal output failed: %v", err)
	}
	if len(bank.Cases) != 1 || bank.Cases[0].Expected != "b" {
		t.Fatalf("unexpected imported case: %+v", bank.Cases)
	}
}

func TestImportReasoningBankGPQA(t *testing.T) {
	tmpDir := t.TempDir()
	inPath := filepath.Join(tmpDir, "gpqa_diamond.csv")
	outPath := filepath.Join(tmpDir, "bank.json")
	csv := "Question,Correct Answer,Incorrect Answer 1,Incorrect Answer 2,Incorrect Answer 3\nWhich gas do plants absorb?,Carbon dioxide,Oxygen,Nitrogen,Hydrogen\n"
	if err := os.WriteFile(inPath, []byte(csv), 0o644); err != nil {
		t.Fatalf("write csv failed: %v", err)
	}

	summary, err := ImportReasoningBank(ReasoningImportConfig{
		InputPath:  inPath,
		OutputPath: outPath,
		Format:     "gpqa_csv",
		Domain:     "graduate_science",
	})
	if err != nil {
		t.Fatalf("ImportReasoningBank gpqa failed: %v", err)
	}
	if summary.CaseCount != 1 {
		t.Fatalf("expected case_count=1, got %d", summary.CaseCount)
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output failed: %v", err)
	}
	var bank reasoningBankEnvelope
	if err := json.Unmarshal(raw, &bank); err != nil {
		t.Fatalf("unmarshal output failed: %v", err)
	}
	if len(bank.Cases) != 1 || bank.Cases[0].Expected == "" {
		t.Fatalf("unexpected imported case: %+v", bank.Cases)
	}
}

func TestResolveReasoningImportFormatAuto(t *testing.T) {
	checks := map[string]string{
		"/tmp/gsm8k_test.jsonl":   "gsm8k_jsonl",
		"/tmp/bbh_task.jsonl":     "bbh_jsonl",
		"/tmp/arc_challenge.json": "arc_jsonl",
		"/tmp/mmlu_math.csv":      "mmlu_csv",
		"/tmp/gpqa_diamond.csv":   "gpqa_csv",
	}
	for path, want := range checks {
		got, err := resolveReasoningImportFormat("auto", path)
		if err != nil {
			t.Fatalf("resolve format failed for %s: %v", path, err)
		}
		if got != want {
			t.Fatalf("resolve format for %s: got %s want %s", path, got, want)
		}
	}
}
