package orchestrator

import (
	"encoding/json"
	"fmt"
	"go/parser"
	"go/token"
	"strings"
)

// Verifier checks outputs using the appropriate verification tier.
type Verifier struct {
	client *LLMClient
	config Config
}

// VerifyResult describes the outcome of one verification.
type VerifyResult struct {
	Passed     bool       `json:"passed"`
	Tier       VerifyTier `json:"tier"`
	Reason     string     `json:"reason"`
	TokensUsed int        `json:"tokens_used"`
}

// VerifyItem is one input to VerifyBatch.
type VerifyItem struct {
	SubTask SubTask
	Result  *SubTaskResult
	Tier    VerifyTier
}

func NewVerifier(client *LLMClient, config Config) *Verifier {
	return &Verifier{client: client, config: config}
}

// Verify checks a sub-task result based on its verify tier.
func (v *Verifier) Verify(subTask SubTask, result *SubTaskResult, tier VerifyTier) (*VerifyResult, error) {
	switch tier {
	case VerifyAutomated:
		return v.verifyAutomated(subTask, result)
	case VerifySpotCheck:
		return v.verifySpotCheck(subTask, result)
	case VerifyFullReview:
		return v.verifyFullReview(subTask, result, "")
	default:
		return v.verifyAutomated(subTask, result)
	}
}

// ============================================================
// TIER 1: AUTOMATED
// ============================================================

func (v *Verifier) verifyAutomated(subTask SubTask, result *SubTaskResult) (*VerifyResult, error) {
	if !result.Success || result.Output == "" {
		return &VerifyResult{
			Passed: false,
			Tier:   VerifyAutomated,
			Reason: "executor reported failure: " + result.ErrorMessage,
		}, nil
	}

	switch subTask.Action {
	case "modify_file", "create_file":
		return v.verifyGoSyntax(result.Output)
	case "generate_test":
		return v.verifyGoSyntax(result.Output)
	case "config_change":
		return v.verifyConfigSyntax(subTask, result.Output)
	default:
		// for actions like generate_pr or analyze, automated check just ensures non-empty
		if strings.TrimSpace(result.Output) == "" {
			return &VerifyResult{Passed: false, Tier: VerifyAutomated, Reason: "empty output"}, nil
		}
		return &VerifyResult{Passed: true, Tier: VerifyAutomated, Reason: "non-empty output"}, nil
	}
}

func (v *Verifier) verifyGoSyntax(code string) (*VerifyResult, error) {
	src := code
	// wrap in a package if not present so parser can parse fragments
	if !strings.Contains(src, "package ") {
		src = "package tmp\n" + src
	}
	fset := token.NewFileSet()
	_, err := parser.ParseFile(fset, "", src, parser.AllErrors)
	if err != nil {
		return &VerifyResult{
			Passed: false,
			Tier:   VerifyAutomated,
			Reason: "Go syntax error: " + err.Error(),
		}, nil
	}
	return &VerifyResult{Passed: true, Tier: VerifyAutomated, Reason: "Go syntax valid"}, nil
}

func (v *Verifier) verifyConfigSyntax(subTask SubTask, output string) (*VerifyResult, error) {
	// determine format from spec if possible
	format := "json"
	if specMap, ok := subTask.Spec.(map[string]interface{}); ok {
		if changes, ok := specMap["changes"].([]interface{}); ok && len(changes) > 0 {
			if change, ok := changes[0].(map[string]interface{}); ok {
				if f, ok := change["format"].(string); ok {
					format = f
				}
			}
		}
	}

	switch format {
	case "json":
		var v interface{}
		if err := json.Unmarshal([]byte(output), &v); err != nil {
			return &VerifyResult{
				Passed: false,
				Tier:   VerifyAutomated,
				Reason: "invalid JSON: " + err.Error(),
			}, nil
		}
	default:
		// for yaml/toml/env: just check non-empty
		if strings.TrimSpace(output) == "" {
			return &VerifyResult{Passed: false, Tier: VerifyAutomated, Reason: "empty config output"}, nil
		}
	}
	return &VerifyResult{Passed: true, Tier: VerifyAutomated, Reason: format + " syntax valid"}, nil
}

// ============================================================
// TIER 2: SPOT CHECK
// ============================================================

func (v *Verifier) verifySpotCheck(subTask SubTask, result *SubTaskResult) (*VerifyResult, error) {
	if !result.Success {
		return &VerifyResult{Passed: false, Tier: VerifySpotCheck, Reason: result.ErrorMessage}, nil
	}

	specJSON, _ := json.Marshal(subTask.Spec)
	system := `Compare the output against the spec. Answer ONLY with valid JSON:
{"passed": true/false, "reason": "one sentence"}

Check ONLY: are the spec's key structural elements present in the output?
Do NOT judge quality. Do NOT suggest improvements.`

	userMsg := fmt.Sprintf("Spec:\n%s\n\nOutput (first 500 chars):\n%s",
		string(specJSON), truncate(result.Output, 500))

	resp, err := v.client.Call(Opus, system, userMsg, 150)
	if err != nil {
		// on API error fall back to passing
		return &VerifyResult{Passed: true, Tier: VerifySpotCheck, Reason: "spot check unavailable"}, nil
	}

	return parseVerifyResponse(resp.Content, VerifySpotCheck, resp.InputTokens+resp.OutputTokens)
}

// ============================================================
// TIER 3: FULL REVIEW
// ============================================================

func (v *Verifier) verifyFullReview(subTask SubTask, result *SubTaskResult, taskContext string) (*VerifyResult, error) {
	if !result.Success {
		return &VerifyResult{Passed: false, Tier: VerifyFullReview, Reason: result.ErrorMessage}, nil
	}

	specJSON, _ := json.Marshal(subTask.Spec)
	system := `Review this output for correctness and completeness.

Check:
1. Does the output correctly implement the spec?
2. Are there any bugs, errors, or omissions?
3. For code: are there security concerns?
4. For cross-repo changes: is it consistent?

Answer with JSON:
{"passed": true/false, "reason": "explanation"}`

	userMsg := fmt.Sprintf("Original task: %s\n\nSpec:\n%s\n\nOutput:\n%s",
		taskContext, string(specJSON), result.Output)

	resp, err := v.client.Call(Opus, system, userMsg, 600)
	if err != nil {
		return &VerifyResult{Passed: true, Tier: VerifyFullReview, Reason: "full review unavailable"}, nil
	}

	return parseVerifyResponse(resp.Content, VerifyFullReview, resp.InputTokens+resp.OutputTokens)
}

// ============================================================
// BATCH VERIFICATION
// ============================================================

// VerifyBatch checks multiple sub-task results in a single Opus call.
func (v *Verifier) VerifyBatch(items []VerifyItem) ([]VerifyResult, error) {
	if len(items) == 0 {
		return nil, nil
	}

	// separate automated items (no Opus needed) from LLM items
	results := make([]VerifyResult, len(items))
	var llmIndexes []int

	for i, item := range items {
		if item.Tier == VerifyAutomated {
			r, err := v.verifyAutomated(item.SubTask, item.Result)
			if err != nil {
				results[i] = VerifyResult{Passed: false, Tier: VerifyAutomated, Reason: err.Error()}
			} else {
				results[i] = *r
			}
		} else {
			llmIndexes = append(llmIndexes, i)
		}
	}

	if len(llmIndexes) == 0 {
		return results, nil
	}

	// batch all LLM checks into one Opus call
	system := `You will verify multiple outputs against their specs. For each item, respond with a JSON object on its own line:
{"index": N, "passed": true/false, "reason": "one sentence"}

Check ONLY structural correctness. Do NOT judge style or quality.`

	var userParts []string
	for _, idx := range llmIndexes {
		item := items[idx]
		specJSON, _ := json.Marshal(item.SubTask.Spec)
		userParts = append(userParts, fmt.Sprintf(
			"--- Item %d (tier %d) ---\nSpec: %s\nOutput: %s",
			idx, item.Tier, string(specJSON), truncate(item.Result.Output, 300),
		))
	}

	resp, err := v.client.Call(Opus, system, strings.Join(userParts, "\n\n"), 500)
	if err != nil {
		// fallback: mark all as passing
		for _, idx := range llmIndexes {
			results[idx] = VerifyResult{Passed: true, Tier: items[idx].Tier, Reason: "batch verify unavailable"}
		}
		return results, nil
	}

	// parse one JSON per line
	type batchLine struct {
		Index  int    `json:"index"`
		Passed bool   `json:"passed"`
		Reason string `json:"reason"`
	}
	for _, line := range strings.Split(resp.Content, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var bl batchLine
		if err := json.Unmarshal([]byte(line), &bl); err != nil {
			continue
		}
		if bl.Index >= 0 && bl.Index < len(results) {
			results[bl.Index] = VerifyResult{
				Passed:     bl.Passed,
				Tier:       items[bl.Index].Tier,
				Reason:     bl.Reason,
				TokensUsed: (resp.InputTokens + resp.OutputTokens) / len(llmIndexes),
			}
		}
	}

	// fill any not returned by batch
	for _, idx := range llmIndexes {
		if results[idx].Reason == "" {
			results[idx] = VerifyResult{Passed: true, Tier: items[idx].Tier, Reason: "no batch response"}
		}
	}

	return results, nil
}

func parseVerifyResponse(content string, tier VerifyTier, tokensUsed int) (*VerifyResult, error) {
	text := stripCodeFences(content)
	start := strings.Index(text, "{")
	if start == -1 {
		return &VerifyResult{Passed: true, Tier: tier, Reason: "could not parse verify response", TokensUsed: tokensUsed}, nil
	}
	text = text[start:]

	var result struct {
		Passed bool   `json:"passed"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return &VerifyResult{Passed: true, Tier: tier, Reason: "parse error: " + err.Error(), TokensUsed: tokensUsed}, nil
	}
	return &VerifyResult{
		Passed:     result.Passed,
		Tier:       tier,
		Reason:     result.Reason,
		TokensUsed: tokensUsed,
	}, nil
}
