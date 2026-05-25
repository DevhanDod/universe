package skills

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// SafetyScanner checks skill instructions for dangerous patterns.
type SafetyScanner struct{}

// NewSafetyScanner creates a new SafetyScanner.
func NewSafetyScanner() *SafetyScanner {
	return &SafetyScanner{}
}

var (
	reURL         = regexp.MustCompile(`(?i)(https?|ftp)://[^\s]+`)
	reCredential  = regexp.MustCompile(`(?i)(api_key|secret|password|api_secret)\s*[:=]\s*["']?[A-Za-z0-9_\-\.]{8,}`)
	rePathTraversal = regexp.MustCompile(`(\.\./|\.\.\\)`)
	reAbsPath     = regexp.MustCompile(`(?i)(/home/|/usr/|/etc/|/var/|/root/|C:\\|D:\\)`)
	reIP          = regexp.MustCompile(`\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b`)
	reVersion     = regexp.MustCompile(`\bv\d+\.\d+\.\d+\b`)

	safeURLPrefixes = []string{
		"https://docs.go.dev", "https://pkg.go.dev",
		"https://golang.org", "https://go.dev",
	}

	dangerousCommands = []string{
		"rm -rf", "curl ", "wget ", "nc ", "ncat ",
		"eval(", "exec(", "> /dev/", "chmod 777", "sudo ",
	}

	injectionPhrases = []string{
		"ignore previous", "ignore all", "you are now",
		"disregard your", "forget your instructions",
		"new role", "jailbreak",
	}
)

// ScanInstruction checks a skill instruction for dangerous patterns.
func (ss *SafetyScanner) ScanInstruction(instruction string) *SafetyScanResult {
	result := &SafetyScanResult{Safe: true}
	lower := strings.ToLower(instruction)

	// BLOCKED: URLs (except safe allowlist)
	for _, match := range reURL.FindAllString(instruction, -1) {
		if !isSafeURL(match) {
			result.Blocked = append(result.Blocked, fmt.Sprintf("external URL detected: %s", match))
		}
	}

	// BLOCKED: credentials
	if reCredential.MatchString(instruction) {
		result.Blocked = append(result.Blocked, "credential reference detected (api_key/secret/password)")
	}

	// BLOCKED: dangerous commands
	for _, cmd := range dangerousCommands {
		if strings.Contains(lower, cmd) {
			result.Blocked = append(result.Blocked, fmt.Sprintf("dangerous command detected: %s", strings.TrimSpace(cmd)))
		}
	}

	// BLOCKED: prompt injection
	for _, phrase := range injectionPhrases {
		if strings.Contains(lower, phrase) {
			result.Blocked = append(result.Blocked, fmt.Sprintf("prompt injection pattern detected: %q", phrase))
		}
	}

	// BLOCKED: path traversal
	if rePathTraversal.MatchString(instruction) {
		result.Blocked = append(result.Blocked, "path traversal detected (../)")
	}

	if len(result.Blocked) > 0 {
		result.Safe = false
	}

	// WARNINGS (non-blocking)
	estimatedTokens := len(strings.Fields(instruction)) * 4 / 3
	if estimatedTokens > 2000 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("instruction is very long (~%d tokens)", estimatedTokens))
	}
	if reAbsPath.MatchString(instruction) {
		result.Warnings = append(result.Warnings, "hardcoded file system paths detected — may break across repos")
	}
	if reIP.MatchString(instruction) {
		result.Warnings = append(result.Warnings, "hardcoded IP address detected — may become stale")
	}
	if reVersion.MatchString(instruction) {
		result.Warnings = append(result.Warnings, "hardcoded version number detected — may become stale")
	}

	return result
}

// ScanEvolutionOutput checks an LLM-generated evolution response.
func (ss *SafetyScanner) ScanEvolutionOutput(output string) (*SafetyScanResult, error) {
	output = strings.TrimSpace(output)

	// Strip code fences
	if strings.HasPrefix(output, "```") {
		if idx := strings.Index(output, "\n"); idx >= 0 {
			output = output[idx+1:]
		}
		output = strings.TrimSuffix(strings.TrimSpace(output), "```")
		output = strings.TrimSpace(output)
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		return &SafetyScanResult{Safe: false, Blocked: []string{"invalid JSON in evolution output"}},
			fmt.Errorf("parse evolution output: %w", err)
	}

	for _, field := range []string{"trigger_desc", "instruction"} {
		if v, ok := parsed[field]; !ok || fmt.Sprint(v) == "" {
			return &SafetyScanResult{Safe: false, Blocked: []string{fmt.Sprintf("missing required field: %s", field)}},
				fmt.Errorf("missing field %s", field)
		}
	}

	instruction, _ := parsed["instruction"].(string)
	result := ss.ScanInstruction(instruction)
	return result, nil
}

func isSafeURL(url string) bool {
	lower := strings.ToLower(url)
	for _, safe := range safeURLPrefixes {
		if strings.HasPrefix(lower, strings.ToLower(safe)) {
			return true
		}
	}
	return false
}
