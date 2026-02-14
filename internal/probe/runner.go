package probe

import (
	"context"
	"strings"
	"time"

	"real-llm/internal/anthropic"
)

type Suite interface {
	Name() string
	Run(ctx context.Context, client *anthropic.Client, cfg RunConfig) Result
}

func AvailableSuites() []Suite {
	return []Suite{
		ParamsSuite{},
		CacheSuite{},
		ToolsSuite{},
		ToolChoiceSuite{},
		StreamSuite{},
		ErrorSuite{},
		AuthenticitySuite{},
		ReasoningSuite{},
		InjectionSuite{},
		LatencySuite{},
		IdentitySuite{},
		NeedleSuite{},
		BlockSizeSuite{},
	}
}

func DefaultSuiteOrder() []string {
	return []string{"params", "cache", "tools", "toolchoice", "stream", "error", "authenticity", "reasoning", "injection", "latency", "identity", "needle", "block"}
}

func ResolveSuiteSelection(selection string) []string {
	value := strings.TrimSpace(strings.ToLower(selection))
	if value == "" || value == "all" {
		return DefaultSuiteOrder()
	}
	items := strings.Split(value, ",")
	names := make([]string, 0, len(items))
	for _, item := range items {
		name := strings.TrimSpace(strings.ToLower(item))
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

func Run(ctx context.Context, client *anthropic.Client, endpoint string, cfg RunConfig, suiteNames []string) Report {
	all := make(map[string]Suite)
	for _, suite := range AvailableSuites() {
		all[suite.Name()] = suite
	}

	results := make([]Result, 0, len(suiteNames))
	for _, name := range suiteNames {
		suite, ok := all[name]
		if !ok {
			results = append(results, Result{
				Suite:   name,
				Status:  StatusFail,
				Summary: "Unknown suite name",
				Error:   "suite not found",
			})
			continue
		}
		start := time.Now()
		result := suite.Run(ctx, client, cfg)
		result.Suite = name
		result.DurationMS = time.Since(start).Milliseconds()
		results = append(results, result)
	}

	report := Report{
		GeneratedAt: time.Now().Format(time.RFC3339),
		Endpoint:    endpoint,
		Model:       cfg.Model,
		Results:     results,
	}
	for _, result := range results {
		switch result.Status {
		case StatusPass:
			report.Passed++
		case StatusWarn:
			report.Warned++
		default:
			report.Failed++
		}
	}
	return report
}
