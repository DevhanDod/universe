package orchestrator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

// LLMClient handles Anthropic API calls for both Opus and Haiku.
type LLMClient struct {
	opusConfig  ModelConfig
	haikuConfig ModelConfig
	httpClient  *http.Client

	opusSem  chan struct{}
	haikuSem chan struct{}

	// per-minute rate tracking
	opusMu       sync.Mutex
	opusCallsMin []time.Time
	haikuMu      sync.Mutex
	haikuCallsMin []time.Time

	opusRPM  int
	haikuRPM int
	backoffMs int
}

// LLMResponse is the parsed response from the Anthropic API.
type LLMResponse struct {
	Content      string
	InputTokens  int
	OutputTokens int
	Model        ModelTier
	LatencyMs    int
	CostUSD      float64
}

func NewLLMClient(opusConfig ModelConfig, haikuConfig ModelConfig) *LLMClient {
	return &LLMClient{
		opusConfig:  opusConfig,
		haikuConfig: haikuConfig,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		opusSem:     make(chan struct{}, max(opusConfig.MaxConcurrent, 1)),
		haikuSem:    make(chan struct{}, max(haikuConfig.MaxConcurrent, 1)),
		opusRPM:     30,
		haikuRPM:    100,
		backoffMs:   1000,
	}
}

// Call makes a request to the Anthropic API.
func (c *LLMClient) Call(model ModelTier, systemPrompt, userMessage string, maxTokens int) (*LLMResponse, error) {
	cfg := c.opusConfig
	sem := c.opusSem
	if model == Haiku {
		cfg = c.haikuConfig
		sem = c.haikuSem
	}
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("no API key configured for model %s", model)
	}

	// acquire concurrency slot
	sem <- struct{}{}
	defer func() { <-sem }()

	// per-minute rate limit
	c.waitForRateLimit(model)

	start := time.Now()
	var resp *LLMResponse
	var err error

	for attempt := 0; attempt < 3; attempt++ {
		resp, err = c.doCall(cfg, systemPrompt, userMessage, maxTokens)
		if err == nil {
			break
		}
		if isRetryable(err) {
			wait := time.Duration(c.backoffMs*(1<<attempt)) * time.Millisecond
			time.Sleep(wait)
			continue
		}
		return nil, err
	}
	if err != nil {
		return nil, err
	}

	resp.Model = model
	resp.LatencyMs = int(time.Since(start).Milliseconds())
	resp.CostUSD = (float64(resp.InputTokens)*cfg.InputCostPerM +
		float64(resp.OutputTokens)*cfg.OutputCostPerM) / 1_000_000
	return resp, nil
}

type anthropicRequest struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Text string `json:"text"`
	} `json:"content"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type retryableError struct{ code int }

func (e retryableError) Error() string { return fmt.Sprintf("HTTP %d", e.code) }

func isRetryable(err error) bool {
	if re, ok := err.(retryableError); ok {
		return re.code == 429 || re.code == 500 || re.code == 502 || re.code == 503
	}
	return false
}

func (c *LLMClient) doCall(cfg ModelConfig, system, user string, maxTokens int) (*LLMResponse, error) {
	body, _ := json.Marshal(anthropicRequest{
		Model:     cfg.ModelID,
		MaxTokens: maxTokens,
		System:    system,
		Messages:  []anthropicMessage{{Role: "user", Content: user}},
	})

	req, err := http.NewRequest(http.MethodPost, cfg.Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", cfg.APIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	httpResp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()

	if httpResp.StatusCode >= 400 {
		return nil, retryableError{code: httpResp.StatusCode}
	}

	raw, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, err
	}

	var ar anthropicResponse
	if err := json.Unmarshal(raw, &ar); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if ar.Error != nil {
		return nil, fmt.Errorf("API error %s: %s", ar.Error.Type, ar.Error.Message)
	}
	if len(ar.Content) == 0 {
		return nil, fmt.Errorf("empty content in response")
	}

	return &LLMResponse{
		Content:      ar.Content[0].Text,
		InputTokens:  ar.Usage.InputTokens,
		OutputTokens: ar.Usage.OutputTokens,
	}, nil
}

func (c *LLMClient) waitForRateLimit(model ModelTier) {
	if model == Opus {
		c.opusMu.Lock()
		defer c.opusMu.Unlock()
		c.opusCallsMin = slidingWindow(c.opusCallsMin, c.opusRPM)
	} else {
		c.haikuMu.Lock()
		defer c.haikuMu.Unlock()
		c.haikuCallsMin = slidingWindow(c.haikuCallsMin, c.haikuRPM)
	}
}

// slidingWindow removes entries older than 1 minute and blocks if at limit.
func slidingWindow(calls []time.Time, limit int) []time.Time {
	now := time.Now()
	cutoff := now.Add(-time.Minute)
	out := calls[:0]
	for _, t := range calls {
		if t.After(cutoff) {
			out = append(out, t)
		}
	}
	for len(out) >= limit {
		time.Sleep(100 * time.Millisecond)
		now = time.Now()
		cutoff = now.Add(-time.Minute)
		fresh := out[:0]
		for _, t := range out {
			if t.After(cutoff) {
				fresh = append(fresh, t)
			}
		}
		out = fresh
	}
	return append(out, now)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
