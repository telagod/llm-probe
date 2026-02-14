package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"real-llm/internal/anthropic"
	"real-llm/internal/probe"
)

func main() {
	baseURL := flag.String("base-url", envOr("CLAUDE_BASE_URL", "https://api.anthropic.com"), "Anthropic-compatible base URL")
	apiKey := flag.String("api-key", envOr("CLAUDE_API_KEY", ""), "API key for endpoint")
	model := flag.String("model", envOr("CLAUDE_MODEL", ""), "Claude model ID")
	version := flag.String("anthropic-version", envOr("ANTHROPIC_VERSION", "2023-06-01"), "anthropic-version request header")
	beta := flag.String("anthropic-beta", envOr("ANTHROPIC_BETA", ""), "anthropic-beta request header (optional)")
	timeout := flag.Duration("timeout", 90*time.Second, "HTTP timeout")
	suites := flag.String("suite", "all", "Comma-separated suites: params,cache,tools,toolchoice,stream,error,authenticity,reasoning,injection,latency,identity,needle,block,all")
	blockStartBytes := flag.Int("block-start-bytes", 65536, "Initial payload size for block suite")
	blockMaxBytes := flag.Int("block-max-bytes", 41943040, "Max payload size for block suite")
	needleStartBytes := flag.Int("needle-start-bytes", 262144, "Initial document bytes for needle-in-haystack suite")
	needleMaxBytes := flag.Int("needle-max-bytes", 16777216, "Max document bytes for needle-in-haystack suite")
	needleRunsPerPos := flag.Int("needle-runs-per-pos", 3, "Regression runs per position for needle suite")
	maxToolRounds := flag.Int("tool-max-rounds", 4, "Max assistant/tool loops for tools suite")
	reasoningRepeat := flag.Int("reasoning-repeat", 2, "Repeat rounds for reasoning consistency checks")
	reasoningBank := flag.String("reasoning-bank", "", "Path to custom reasoning bank JSON (supports envelope schema or legacy array)")
	reasoningDomains := flag.String("reasoning-domains", "all", "Comma-separated professional domains for reasoning suite, e.g. medicine,finance,law")
	reasoningMaxCases := flag.Int("reasoning-max-cases", 32, "Max reasoning cases sampled from prompt bank")
	reasoningDomainWarn := flag.Float64("reasoning-domain-warn", 0.8, "Warn threshold for per-domain reasoning accuracy")
	reasoningDomainFail := flag.Float64("reasoning-domain-fail", 0.6, "Fail threshold for per-domain reasoning accuracy")
	reasoningWeightedWarn := flag.Float64("reasoning-weighted-warn", 0.8, "Warn threshold for weighted reasoning score")
	reasoningWeightedFail := flag.Float64("reasoning-weighted-fail", 0.65, "Fail threshold for weighted reasoning score")
	reasoningImportIn := flag.String("reasoning-import-in", "", "Import source file path for external reasoning benchmark mapping")
	reasoningImportOut := flag.String("reasoning-import-out", "", "Output path for generated reasoning bank JSON")
	reasoningImportFormat := flag.String("reasoning-import-format", "auto", "Import format: auto|gsm8k_jsonl|bbh_jsonl|arc_jsonl|mmlu_csv|gpqa_csv")
	reasoningImportDomain := flag.String("reasoning-import-domain", "", "Domain label for imported reasoning cases")
	reasoningImportName := flag.String("reasoning-import-name", "", "Optional bank name for imported reasoning bank")
	reasoningImportSource := flag.String("reasoning-import-source", "", "Optional source string for imported reasoning bank metadata")
	deepProbe := flag.Bool("deep-probe", true, "Enable deeper hard-to-spoof probes")
	forensicsLevel := flag.String("forensics-level", "balanced", "Forensics intensity: fast|balanced|forensic")
	consistencyRuns := flag.Int("consistency-runs", 0, "Consistency probe rounds (0=auto by forensics-level)")
	consistencyDriftWarn := flag.Float64("consistency-drift-warn", 0, "Warn threshold for consistency drift score in percent (0=auto)")
	consistencyDriftFail := flag.Float64("consistency-drift-fail", 0, "Fail threshold for consistency drift score in percent (0=auto)")
	enableTrustScore := flag.Bool("trust-score", true, "Append weighted multi-dimensional trust score result")
	hardGate := flag.Bool("hard-gate", true, "Enable hard-gate precedence for critical spoof/injection signals")
	hardGateStreamFail := flag.Bool("hard-gate-stream-fail", false, "Treat stream suite failures as hard-gate triggers")
	hardGateErrorFail := flag.Bool("hard-gate-error-fail", false, "Treat error suite failures as hard-gate triggers")
	hardGateSpoofRisk := flag.Float64("hard-gate-spoof-risk", 70, "spoof_risk_score threshold for hard-gate fail")
	scoreWeightAuthenticity := flag.Float64("score-weight-authenticity", 0.30, "Weight for authenticity dimension")
	scoreWeightInjection := flag.Float64("score-weight-injection", 0.25, "Weight for injection dimension")
	scoreWeightTools := flag.Float64("score-weight-tools", 0.15, "Weight for tools dimension")
	scoreWeightToolChoice := flag.Float64("score-weight-toolchoice", 0.10, "Weight for toolchoice dimension")
	scoreWeightStream := flag.Float64("score-weight-stream", 0.10, "Weight for stream dimension")
	scoreWeightError := flag.Float64("score-weight-error", 0.10, "Weight for error dimension")
	scoreWeightLatency := flag.Float64("score-weight-latency", 0.15, "Weight for latency dimension")
	latencyRounds := flag.Int("latency-rounds", 0, "Latency probe rounds (0=auto by forensics-level)")
	identityRounds := flag.Int("identity-rounds", 0, "Extra latency sampling rounds for identity suite (0=auto)")
	identitySeed := flag.Int64("identity-seed", 0, "Seed for identity suite question generation (0=random)")
	scoreWeightIdentity := flag.Float64("score-weight-identity", 0.15, "Weight for identity dimension")
	scoreWarnThreshold := flag.Float64("score-warn-threshold", 75, "Warn threshold for weighted trust score")
	scoreFailThreshold := flag.Float64("score-fail-threshold", 60, "Fail threshold for weighted trust score")
	format := flag.String("format", "text", "Output format: text|json")
	outputPath := flag.String("out", "", "Write full report JSON to this file")
	baselineInPath := flag.String("baseline-in", "", "Load baseline report JSON and run drift comparison")
	baselineOutPath := flag.String("baseline-out", "", "Write current report as future baseline JSON")
	historyGlob := flag.String("history-glob", "", "Glob pattern of historical report JSON files for timeline analysis")
	historyMax := flag.Int("history-max", 200, "Max historical reports loaded for timeline analysis")
	timelineOutPath := flag.String("timeline-out", "", "Write timeline snapshot JSON to this file")
	strict := flag.Bool("strict", false, "Exit non-zero if any suite is warn/fail")
	flag.Parse()

	if strings.TrimSpace(*reasoningImportIn) != "" {
		summary, err := probe.ImportReasoningBank(probe.ReasoningImportConfig{
			InputPath:  *reasoningImportIn,
			OutputPath: *reasoningImportOut,
			Format:     *reasoningImportFormat,
			Domain:     *reasoningImportDomain,
			Name:       *reasoningImportName,
			Source:     *reasoningImportSource,
		})
		if err != nil {
			exitWith("failed to import reasoning bank: " + err.Error())
		}
		switch strings.ToLower(strings.TrimSpace(*format)) {
		case "json":
			data, marshalErr := json.MarshalIndent(summary, "", "  ")
			if marshalErr != nil {
				exitWith("failed to encode import summary: " + marshalErr.Error())
			}
			fmt.Println(string(data))
		default:
			printImportSummary(summary)
		}
		return
	}

	if strings.TrimSpace(*apiKey) == "" {
		exitWith("CLAUDE_API_KEY or -api-key is required")
	}
	if strings.TrimSpace(*model) == "" {
		exitWith("CLAUDE_MODEL or -model is required")
	}

	client := anthropic.NewClient(anthropic.Config{
		BaseURL:          *baseURL,
		APIKey:           *apiKey,
		AnthropicVersion: *version,
		AnthropicBeta:    *beta,
		Timeout:          *timeout,
	})

	runConfig := probe.RunConfig{
		Model:                   *model,
		BlockStartBytes:         *blockStartBytes,
		BlockMaxBytes:           *blockMaxBytes,
		MaxToolRounds:           *maxToolRounds,
		DeepProbe:               *deepProbe,
		ForensicsLevel:          *forensicsLevel,
		ConsistencyRuns:         *consistencyRuns,
		ConsistencyDriftWarn:    *consistencyDriftWarn,
		ConsistencyDriftFail:    *consistencyDriftFail,
		EnableTrustScore:        *enableTrustScore,
		HardGate:                *hardGate,
		HardGateStreamFail:      *hardGateStreamFail,
		HardGateErrorFail:       *hardGateErrorFail,
		HardGateSpoofRisk:       *hardGateSpoofRisk,
		ScoreWeightAuthenticity: *scoreWeightAuthenticity,
		ScoreWeightInjection:    *scoreWeightInjection,
		ScoreWeightTools:        *scoreWeightTools,
		ScoreWeightToolChoice:   *scoreWeightToolChoice,
		ScoreWeightStream:       *scoreWeightStream,
		ScoreWeightError:        *scoreWeightError,
		ScoreWeightLatency:      *scoreWeightLatency,
		LatencyRounds:           *latencyRounds,
		ScoreWarnThreshold:      *scoreWarnThreshold,
		ScoreFailThreshold:      *scoreFailThreshold,
		ReasoningBankPath:       *reasoningBank,
		ReasoningRepeat:         *reasoningRepeat,
		ReasoningDomains:        *reasoningDomains,
		ReasoningMaxCases:       *reasoningMaxCases,
		ReasoningDomainWarn:     *reasoningDomainWarn,
		ReasoningDomainFail:     *reasoningDomainFail,
		ReasoningWeightedWarn:   *reasoningWeightedWarn,
		ReasoningWeightedFail:   *reasoningWeightedFail,
		NeedleStartBytes:        *needleStartBytes,
		NeedleMaxBytes:          *needleMaxBytes,
		NeedleRunsPerPos:        *needleRunsPerPos,
		IdentityRounds:          *identityRounds,
		IdentitySeed:            *identitySeed,
		ScoreWeightIdentity:     *scoreWeightIdentity,
	}

	ctx, cancel := context.WithTimeout(context.Background(), *timeout*8)
	defer cancel()

	selected := probe.ResolveSuiteSelection(*suites)
	report := probe.Run(ctx, client, *baseURL, runConfig, selected)

	if strings.TrimSpace(*baselineInPath) != "" {
		baseline, err := readReport(*baselineInPath)
		if err != nil {
			exitWith("failed to read baseline report: " + err.Error())
		}
		regression := probe.CompareWithBaseline(report, baseline)
		probe.AppendResult(&report, regression)
	}

	if strings.TrimSpace(*historyGlob) != "" || strings.TrimSpace(*timelineOutPath) != "" {
		historyReports := []probe.Report{}
		if strings.TrimSpace(*historyGlob) != "" {
			loaded, err := readReportsByGlob(*historyGlob, *historyMax)
			if err != nil {
				exitWith("failed to load history reports: " + err.Error())
			}
			historyReports = loaded
		}
		timelineResult, timelineSnapshot := probe.AnalyzeTimeline(historyReports, report)
		probe.AppendResult(&report, timelineResult)

		if strings.TrimSpace(*timelineOutPath) != "" {
			if err := writeJSON(*timelineOutPath, timelineSnapshot); err != nil {
				exitWith("failed to write timeline snapshot: " + err.Error())
			}
		}
	}

	if runConfig.EnableTrustScore {
		trustScoreResult := probe.BuildTrustScoreResult(report, runConfig)
		probe.AppendResult(&report, trustScoreResult)
	}

	switch strings.ToLower(strings.TrimSpace(*format)) {
	case "json":
		printJSON(report)
	default:
		printText(report)
	}

	if strings.TrimSpace(*outputPath) != "" {
		if err := writeReport(*outputPath, report); err != nil {
			exitWith("failed to write report: " + err.Error())
		}
	}
	if strings.TrimSpace(*baselineOutPath) != "" {
		if err := writeReport(*baselineOutPath, report); err != nil {
			exitWith("failed to write baseline report: " + err.Error())
		}
	}

	if *strict && (report.Warned > 0 || report.Failed > 0) {
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func printText(report probe.Report) {
	fmt.Printf("Endpoint: %s\n", report.Endpoint)
	fmt.Printf("Model: %s\n", report.Model)
	fmt.Printf("Generated: %s\n\n", report.GeneratedAt)

	for _, result := range report.Results {
		fmt.Printf("[%s] %s - %s (%dms)\n", strings.ToUpper(string(result.Status)), result.Suite, result.Summary, result.DurationMS)
		if result.Error != "" {
			fmt.Printf("  error: %s\n", result.Error)
		}
		for _, finding := range result.Findings {
			fmt.Printf("  - %s\n", finding)
		}
		if len(result.Metrics) > 0 {
			metricsJSON, _ := json.Marshal(result.Metrics)
			fmt.Printf("  metrics: %s\n", metricsJSON)
		}
		fmt.Println()
	}

	fmt.Printf("Totals: pass=%d warn=%d fail=%d\n", report.Passed, report.Warned, report.Failed)
}

func printJSON(report probe.Report) {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		exitWith("failed to encode report JSON: " + err.Error())
	}
	fmt.Println(string(data))
}

func writeReport(path string, report probe.Report) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	cleanPath := filepath.Clean(path)
	return os.WriteFile(cleanPath, data, 0o644)
}

func writeJSON(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	cleanPath := filepath.Clean(path)
	return os.WriteFile(cleanPath, data, 0o644)
}

func printImportSummary(summary probe.ReasoningImportSummary) {
	fmt.Printf("Reasoning bank imported\n")
	fmt.Printf("  format: %s\n", summary.Format)
	fmt.Printf("  input: %s\n", summary.InputPath)
	fmt.Printf("  output: %s\n", summary.OutputPath)
	fmt.Printf("  version: %s\n", summary.Version)
	fmt.Printf("  name: %s\n", summary.Name)
	fmt.Printf("  source: %s\n", summary.Source)
	fmt.Printf("  cases: %d\n", summary.CaseCount)
	if len(summary.Domains) > 0 {
		fmt.Printf("  domains: %s\n", strings.Join(summary.Domains, ","))
	}
}

func readReport(path string) (probe.Report, error) {
	cleanPath := filepath.Clean(path)
	data, err := os.ReadFile(cleanPath)
	if err != nil {
		return probe.Report{}, err
	}
	var report probe.Report
	if err := json.Unmarshal(data, &report); err != nil {
		return probe.Report{}, err
	}
	return report, nil
}

func readReportsByGlob(pattern string, maxCount int) ([]probe.Report, error) {
	cleanPattern := filepath.Clean(pattern)
	matches, err := filepath.Glob(cleanPattern)
	if err != nil {
		return nil, err
	}
	if maxCount <= 0 {
		maxCount = 200
	}
	reports := make([]probe.Report, 0, len(matches))
	for _, path := range matches {
		if len(reports) >= maxCount {
			break
		}
		report, readErr := readReport(path)
		if readErr != nil {
			continue
		}
		if len(report.Results) == 0 {
			continue
		}
		reports = append(reports, report)
	}
	return reports, nil
}

func exitWith(message string) {
	fmt.Fprintln(os.Stderr, "error:", message)
	os.Exit(2)
}
