package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Config struct {
	BaseURL          string
	APIKey           string
	AnthropicVersion string
	AnthropicBeta    string
	Timeout          time.Duration
}

type RequestOptions struct {
	OmitAPIKey   bool
	OmitVersion  bool
	OmitBeta     bool
	ExtraHeaders map[string]string
}

type RawResponse struct {
	StatusCode int
	Headers    http.Header
	Body       []byte
	Duration   time.Duration
}

func (r *RawResponse) Header(name string) string {
	if r == nil {
		return ""
	}
	return r.Headers.Get(name)
}

type Client struct {
	baseURL string
	apiKey  string
	version string
	beta    string
	client  *http.Client
}

func NewClient(cfg Config) *Client {
	baseURL := strings.TrimRight(cfg.BaseURL, "/")
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	version := cfg.AnthropicVersion
	if version == "" {
		version = "2023-06-01"
	}
	return &Client{
		baseURL: baseURL,
		apiKey:  cfg.APIKey,
		version: version,
		beta:    cfg.AnthropicBeta,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

func (c *Client) CreateMessage(ctx context.Context, req MessageRequest) (*MessageResponse, *RawResponse, error) {
	raw, err := c.RawRequest(ctx, http.MethodPost, "/v1/messages", req, RequestOptions{})
	if err != nil {
		return nil, raw, err
	}

	var resp MessageResponse
	if err := json.Unmarshal(raw.Body, &resp); err != nil {
		return nil, raw, fmt.Errorf("decode message response: %w", err)
	}
	return &resp, raw, nil
}

func (c *Client) ListModels(ctx context.Context) (*ModelsResponse, *RawResponse, error) {
	raw, err := c.RawRequest(ctx, http.MethodGet, "/v1/models", nil, RequestOptions{})
	if err != nil {
		return nil, raw, err
	}

	var resp ModelsResponse
	if err := json.Unmarshal(raw.Body, &resp); err != nil {
		return nil, raw, fmt.Errorf("decode models response: %w", err)
	}
	return &resp, raw, nil
}

func (c *Client) RawRequest(ctx context.Context, method, path string, body any, opts RequestOptions) (*RawResponse, error) {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		payload = b
	}
	return c.rawRequestWithPayload(ctx, method, path, payload, opts)
}

func (c *Client) RawPayloadRequest(ctx context.Context, method, path string, payload []byte, opts RequestOptions) (*RawResponse, error) {
	return c.rawRequestWithPayload(ctx, method, path, payload, opts)
}

func (c *Client) rawRequestWithPayload(ctx context.Context, method, path string, payload []byte, opts RequestOptions) (*RawResponse, error) {
	fullURL := c.baseURL + path
	var reader io.Reader
	if len(payload) > 0 {
		reader = bytes.NewReader(payload)
	}

	request, err := http.NewRequestWithContext(ctx, method, fullURL, reader)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}

	if len(payload) > 0 {
		request.Header.Set("Content-Type", "application/json")
	}
	if !opts.OmitAPIKey && c.apiKey != "" {
		request.Header.Set("x-api-key", c.apiKey)
	}
	if !opts.OmitVersion && c.version != "" {
		request.Header.Set("anthropic-version", c.version)
	}
	if !opts.OmitBeta && c.beta != "" {
		request.Header.Set("anthropic-beta", c.beta)
	}
	for k, v := range opts.ExtraHeaders {
		if v == "" {
			request.Header.Del(k)
			continue
		}
		request.Header.Set(k, v)
	}

	start := time.Now()
	response, err := c.client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer response.Body.Close()

	bodyBytes, readErr := io.ReadAll(response.Body)
	raw := &RawResponse{
		StatusCode: response.StatusCode,
		Headers:    response.Header.Clone(),
		Body:       bodyBytes,
		Duration:   time.Since(start),
	}
	if readErr != nil {
		return raw, fmt.Errorf("read response body: %w", readErr)
	}

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		envelope, ok := ParseAPIErrorEnvelope(bodyBytes)
		if !ok {
			return raw, fmt.Errorf("api status %d: %s", response.StatusCode, string(bodyBytes))
		}
		return raw, &APIError{
			StatusCode: response.StatusCode,
			Envelope:   envelope,
			Body:       bodyBytes,
		}
	}
	return raw, nil
}

func IsAPIError(err error) (*APIError, bool) {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}
