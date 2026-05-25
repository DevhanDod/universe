package memory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Compressor uses a cheap LLM to convert raw session events into observation summaries.
type Compressor struct {
	config Config
	client *http.Client
}

// NewCompressor creates a new Compressor.
func NewCompressor(config Config) *Compressor {
	return &Compressor{
		config: config,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// CompressEvents takes raw session events and produces Observations (without embeddings).
func (c *Compressor) CompressEvents(events []SessionEvent, developerID, repoID, sessionID string) ([]Observation, error) {
	// Group events by graph_node_id
	grouped := make(map[string][]SessionEvent)
	order := []string{}
	for _, e := range events {
		key := e.GraphNodeID
		if key == "" {
			key = "__untagged__"
		}
		if _, ok := grouped[key]; !ok {
			order = append(order, key)
		}
		grouped[key] = append(grouped[key], e)
	}

	var observations []Observation
	for _, key := range order {
		group := grouped[key]
		obs, err := c.compressGroup(group, key, developerID, repoID, sessionID)
		if err != nil {
			// Fallback: create minimal observation from first event
			obs = c.fallbackObservation(group[0], developerID, repoID, sessionID)
		}
		observations = append(observations, *obs)
	}
	return observations, nil
}

// CompressSingleEvent compresses a single event into an observation.
func (c *Compressor) CompressSingleEvent(event SessionEvent, developerID, repoID, sessionID string) (*Observation, error) {
	return c.compressGroup([]SessionEvent{event}, event.GraphNodeID, developerID, repoID, sessionID)
}

func (c *Compressor) compressGroup(events []SessionEvent, graphNodeID, developerID, repoID, sessionID string) (*Observation, error) {
	eventsJSON, err := json.MarshalIndent(events, "", "  ")
	if err != nil {
		return nil, err
	}

	systemPrompt := `Compress these agent session events into a concise technical observation.

RULES:
- Output ONE JSON object, nothing else
- "summary": 1-2 sentences, max 100 tokens. Keep function names, error types, and the fix approach. Drop everything else.
- "detail": 3-5 sentences, max 500 tokens. Include the full context: what was wrong, what was tried, what worked, what code changed.
- "category": one of "fix", "pattern", "decision", "failure", "convention"

Respond with ONLY the JSON:
{"summary": "...", "detail": "...", "category": "..."}`

	userMessage := "EVENTS:\n" + string(eventsJSON)

	responseText, err := c.callHaiku(systemPrompt, userMessage, 400)
	if err != nil {
		return nil, err
	}

	obs, err := c.parseCompressedResponse(responseText)
	if err != nil {
		// Retry with stricter prompt
		strictPrompt := systemPrompt + "\n\nIMPORTANT: Respond with ONLY valid JSON. No markdown. No explanation."
		responseText2, err2 := c.callHaiku(strictPrompt, userMessage, 400)
		if err2 != nil {
			return nil, fmt.Errorf("compress retry failed: %w", err2)
		}
		obs, err = c.parseCompressedResponse(responseText2)
		if err != nil {
			return c.fallbackObservation(events[0], developerID, repoID, sessionID), nil
		}
	}

	obs.DeveloperID = developerID
	obs.RepoID = repoID
	obs.GraphNodeID = graphNodeID
	obs.SessionID = sessionID
	obs.Confidence = 1.0

	// Collect tool calls from events
	for _, e := range events {
		if e.ToolName != "" {
			obs.ToolCalls = append(obs.ToolCalls, ToolCall{
				Tool:   e.ToolName,
				Target: e.GraphNodeID,
			})
		}
	}

	return obs, nil
}

func (c *Compressor) parseCompressedResponse(text string) (*Observation, error) {
	text = strings.TrimSpace(text)
	// Strip markdown code fences if present
	if strings.HasPrefix(text, "```") {
		if idx := strings.Index(text, "\n"); idx >= 0 {
			text = text[idx+1:]
		}
		if strings.HasSuffix(strings.TrimSpace(text), "```") {
			text = strings.TrimSpace(text)
			text = text[:len(text)-3]
		}
		text = strings.TrimSpace(text)
	}

	var parsed struct {
		Summary  string `json:"summary"`
		Detail   string `json:"detail"`
		Category string `json:"category"`
	}
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		return nil, fmt.Errorf("parse compressed response: %w", err)
	}
	if parsed.Summary == "" {
		return nil, fmt.Errorf("empty summary in compressed response")
	}

	validCategories := map[string]bool{
		"fix": true, "pattern": true, "decision": true,
		"failure": true, "convention": true,
	}
	if !validCategories[parsed.Category] {
		parsed.Category = "pattern"
	}

	return &Observation{
		Summary:  parsed.Summary,
		Detail:   parsed.Detail,
		Category: parsed.Category,
	}, nil
}

func (c *Compressor) fallbackObservation(event SessionEvent, developerID, repoID, sessionID string) *Observation {
	summary := event.ToolName
	if event.Input != "" {
		if len(event.Input) > 100 {
			summary = event.Input[:100]
		} else {
			summary = event.Input
		}
	}
	category := "pattern"
	if !event.Success {
		category = "failure"
	}
	return &Observation{
		DeveloperID: developerID,
		RepoID:      repoID,
		GraphNodeID: event.GraphNodeID,
		SessionID:   sessionID,
		Category:    category,
		Summary:     summary,
		Confidence:  1.0,
	}
}

func (c *Compressor) callHaiku(systemPrompt, userMessage string, maxTokens int) (string, error) {
	reqBody, err := json.Marshal(map[string]interface{}{
		"model":      c.config.CompressorModel,
		"max_tokens": maxTokens,
		"system":     systemPrompt,
		"messages": []map[string]string{
			{"role": "user", "content": userMessage},
		},
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequest("POST", c.config.CompressorEndpoint, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.config.CompressorAPIKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("haiku request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("haiku HTTP %d", resp.StatusCode)
	}

	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
		Error *struct{ Message string } `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode haiku response: %w", err)
	}
	if result.Error != nil {
		return "", fmt.Errorf("haiku error: %s", result.Error.Message)
	}
	if len(result.Content) == 0 {
		return "", fmt.Errorf("empty haiku response")
	}
	return result.Content[0].Text, nil
}
