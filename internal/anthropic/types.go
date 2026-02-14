package anthropic

import "encoding/json"

type CacheControl struct {
	Type string `json:"type"`
	TTL  string `json:"ttl,omitempty"`
}

type ContentBlock struct {
	Type         string          `json:"type"`
	Text         string          `json:"text,omitempty"`
	ID           string          `json:"id,omitempty"`
	Name         string          `json:"name,omitempty"`
	Input        json.RawMessage `json:"input,omitempty"`
	ToolUseID    string          `json:"tool_use_id,omitempty"`
	Content      any             `json:"content,omitempty"`
	IsError      bool            `json:"is_error,omitempty"`
	CacheControl *CacheControl   `json:"cache_control,omitempty"`
	Thinking     string          `json:"thinking,omitempty"`
	Signature    string          `json:"signature,omitempty"`
}

type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

type ThinkingConfig struct {
	Type         string `json:"type"`
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

type MessageRequest struct {
	Model         string           `json:"model"`
	MaxTokens     int              `json:"max_tokens"`
	Messages      []Message        `json:"messages"`
	System        any              `json:"system,omitempty"`
	Metadata      map[string]any   `json:"metadata,omitempty"`
	StopSequences []string         `json:"stop_sequences,omitempty"`
	Stream        bool             `json:"stream,omitempty"`
	Temperature   *float64         `json:"temperature,omitempty"`
	TopP          *float64         `json:"top_p,omitempty"`
	TopK          *int             `json:"top_k,omitempty"`
	Tools         []ToolDefinition `json:"tools,omitempty"`
	ToolChoice    any              `json:"tool_choice,omitempty"`
	Thinking      *ThinkingConfig  `json:"thinking,omitempty"`
	ServiceTier   string           `json:"service_tier,omitempty"`
}

type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

type MessageResponse struct {
	ID           string         `json:"id"`
	Type         string         `json:"type"`
	Role         string         `json:"role"`
	Model        string         `json:"model"`
	Content      []ContentBlock `json:"content"`
	StopReason   string         `json:"stop_reason"`
	StopSequence *string        `json:"stop_sequence"`
	Usage        Usage          `json:"usage"`
}

type APIErrorEnvelope struct {
	Type      string         `json:"type"`
	Error     APIErrorDetail `json:"error"`
	RequestID string         `json:"request_id"`
}

type APIErrorDetail struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

type APIError struct {
	StatusCode int
	Envelope   APIErrorEnvelope
	Body       []byte
}

func (e *APIError) Error() string {
	if e == nil {
		return ""
	}
	if e.Envelope.Error.Message != "" {
		return e.Envelope.Error.Type + ": " + e.Envelope.Error.Message
	}
	return string(e.Body)
}

type Model struct {
	Type        string `json:"type"`
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
	CreatedAt   string `json:"created_at"`
}

type ModelsResponse struct {
	Data    []Model `json:"data"`
	HasMore bool    `json:"has_more"`
	FirstID string  `json:"first_id"`
	LastID  string  `json:"last_id"`
}

func ParseAPIErrorEnvelope(body []byte) (APIErrorEnvelope, bool) {
	var envelope APIErrorEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return APIErrorEnvelope{}, false
	}
	if envelope.Error.Type == "" && envelope.Error.Message == "" {
		return APIErrorEnvelope{}, false
	}
	return envelope, true
}
