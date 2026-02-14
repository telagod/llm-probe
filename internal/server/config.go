package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type ServerConfig struct {
	ListenAddr string               `json:"listen_addr" yaml:"listen_addr"`
	Database   DatabaseConfig       `json:"database" yaml:"database"`
	Auth       AuthConfig           `json:"auth" yaml:"auth"`
	Security   SecurityConfig       `json:"security" yaml:"security"`
	Keys       KeyPoolConfig        `json:"keys" yaml:"keys"`
	Budget     BudgetConfig         `json:"budget" yaml:"budget"`
	Observer   ObservabilityConfig  `json:"observability" yaml:"observability"`
	Limits     UserQuickLimitConfig `json:"limits" yaml:"limits"`
}

type DatabaseConfig struct {
	DSN            string `json:"dsn" yaml:"dsn"`
	MaxConns       int32  `json:"max_conns" yaml:"max_conns"`
	MigrationsPath string `json:"migrations_path" yaml:"migrations_path"`
}

type AuthConfig struct {
	SessionTTL string `json:"session_ttl" yaml:"session_ttl"`
	CookieName string `json:"cookie_name" yaml:"cookie_name"`
}

type SecurityConfig struct {
	AdminAllowedDomains []string `json:"admin_allowed_domains" yaml:"admin_allowed_domains"`
	AdminToken          string   `json:"admin_token" yaml:"admin_token"`
}

type KeyPoolConfig struct {
	TestKeys []TestKeyConfig `json:"test_key_pool" yaml:"test_key_pool"`
}

type TestKeyConfig struct {
	Label           string  `json:"label" yaml:"label"`
	APIKey          string  `json:"api_key" yaml:"api_key"`
	DailyLimitUSD   float64 `json:"daily_limit_usd" yaml:"daily_limit_usd"`
	RPM             int     `json:"rpm" yaml:"rpm"`
	TPM             int     `json:"tpm" yaml:"tpm"`
	InputCostPer1K  float64 `json:"input_cost_per_1k" yaml:"input_cost_per_1k"`
	OutputCostPer1K float64 `json:"output_cost_per_1k" yaml:"output_cost_per_1k"`
}

type BudgetConfig struct {
	DefaultRunMaxUSD  float64 `json:"default_run_max_usd" yaml:"default_run_max_usd"`
	DefaultTimeoutSec int     `json:"default_timeout_sec" yaml:"default_timeout_sec"`
	MaxParallelRuns   int     `json:"max_parallel_runs" yaml:"max_parallel_runs"`
}

type ObservabilityConfig struct {
	OTLPEndpoint string  `json:"otlp_endpoint" yaml:"otlp_endpoint"`
	ServiceName  string  `json:"service_name" yaml:"service_name"`
	SampleRatio  float64 `json:"sample_ratio" yaml:"sample_ratio"`
}

type UserQuickLimitConfig struct {
	QuickTestRPM int `json:"quick_test_rpm" yaml:"quick_test_rpm"`
}

func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		ListenAddr: ":8080",
		Database: DatabaseConfig{
			MaxConns:       10,
			MigrationsPath: "./migrations",
		},
		Auth: AuthConfig{
			SessionTTL: "8h",
			CookieName: "probe_session",
		},
		Budget: BudgetConfig{
			DefaultRunMaxUSD:  5,
			DefaultTimeoutSec: 540,
			MaxParallelRuns:   2,
		},
		Observer: ObservabilityConfig{
			ServiceName: "probe-api",
			SampleRatio: 1,
		},
		Limits: UserQuickLimitConfig{
			QuickTestRPM: 6,
		},
	}
}

func LoadServerConfig(path string) (ServerConfig, error) {
	cfg := DefaultServerConfig()
	if strings.TrimSpace(path) == "" {
		return cfg, nil
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".yaml", ".yml":
		if err := yaml.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("parse yaml config: %w", err)
		}
	case ".json":
		if err := json.Unmarshal(data, &cfg); err != nil {
			return cfg, fmt.Errorf("parse json config: %w", err)
		}
	default:
		var yamlErr error
		if yamlErr = yaml.Unmarshal(data, &cfg); yamlErr == nil {
			break
		}
		if err := json.Unmarshal(data, &cfg); err != nil {
			return cfg, errors.New("config format not recognized (expected yaml/json)")
		}
	}
	normalizeConfig(&cfg)
	return cfg, nil
}

func normalizeConfig(cfg *ServerConfig) {
	if cfg == nil {
		return
	}
	if strings.TrimSpace(cfg.ListenAddr) == "" {
		cfg.ListenAddr = ":8080"
	}
	if cfg.Database.MaxConns <= 0 {
		cfg.Database.MaxConns = 10
	}
	if strings.TrimSpace(cfg.Database.MigrationsPath) == "" {
		cfg.Database.MigrationsPath = "./migrations"
	}
	if strings.TrimSpace(cfg.Auth.CookieName) == "" {
		cfg.Auth.CookieName = "probe_session"
	}
	if strings.TrimSpace(cfg.Auth.SessionTTL) == "" {
		cfg.Auth.SessionTTL = "8h"
	}
	if cfg.Budget.DefaultRunMaxUSD <= 0 {
		cfg.Budget.DefaultRunMaxUSD = 5
	}
	if cfg.Budget.DefaultTimeoutSec <= 0 {
		cfg.Budget.DefaultTimeoutSec = 540
	}
	if cfg.Budget.MaxParallelRuns <= 0 {
		cfg.Budget.MaxParallelRuns = 2
	}
	if cfg.Observer.SampleRatio <= 0 || cfg.Observer.SampleRatio > 1 {
		cfg.Observer.SampleRatio = 1
	}
	if strings.TrimSpace(cfg.Observer.ServiceName) == "" {
		cfg.Observer.ServiceName = "probe-api"
	}
	if cfg.Limits.QuickTestRPM <= 0 {
		cfg.Limits.QuickTestRPM = 6
	}
}
