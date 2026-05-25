package compress

import "strings"

type CompressionLevel string

const (
	LevelFull    CompressionLevel = "full"
	LevelCompact CompressionLevel = "compact"
	LevelNormal  CompressionLevel = "normal"
)

const compactPrompt = `COMMUNICATION RULES — FOLLOW STRICTLY:
- Drop articles (a, an, the) from explanations
- No pleasantries: never say "I'd be happy to help", "Sure, let me", "Great question"
- No hedging: never say "It might be worth considering", "You may want to", "Perhaps"
- No filler: never say "Let me explain", "As you can see", "It's important to note"
- No apologies: never say "Sorry", "Unfortunately", "I apologize"
- Keep ALL code blocks, function names, variable names, error messages EXACT — never compress code
- Keep ALL technical terms exact — never simplify jargon
- Use graph shorthand when referencing code entities (provided below)
- Max 2 sentences for explanations unless specifically asked for detail
- If code alone answers the question, output code only — no surrounding prose
- Git commit messages, PR titles, PR descriptions: write normally (not compressed)
`

const normalPrompt = `COMMUNICATION RULES:
- Do not start responses with "I'd be happy to help", "Sure!", "Great question!", or similar
- Do not hedge with "It might be worth considering" or "You may want to"
- Be direct. State what is, not what might be.
- Keep code blocks and technical terms exact
- Use graph shorthand when referencing code entities (provided below)
`

const fullPrompt = `OUTPUT FORMAT — STRICT:
- Respond ONLY with valid JSON matching the provided schema
- No explanation before or after the JSON
- No markdown code fences around the JSON
- No preamble, no summary, no sign-off
- Every token must be part of the JSON structure
- If you cannot complete the task, respond with: {"error": "description"}
`

type TaskType string

const (
	TaskFix      TaskType = "fix"
	TaskTest     TaskType = "test"
	TaskPR       TaskType = "pr"
	TaskAnalysis TaskType = "analysis"
	TaskGeneral  TaskType = "general"
)

type PromptConfig struct {
	Level        CompressionLevel
	GraphContext []GraphNodeInfo
	OutputSchema string
	TaskType     TaskType
}

func BuildPrompt(basePrompt string, config PromptConfig) string {
	var b strings.Builder

	switch config.Level {
	case LevelFull:
		b.WriteString(fullPrompt)
	case LevelNormal:
		b.WriteString(normalPrompt)
	default:
		b.WriteString(compactPrompt)
	}

	if len(config.GraphContext) > 0 {
		b.WriteString("\nGRAPH CONTEXT (use as shorthand):\n")
		if config.Level == LevelFull {
			b.WriteString(BuildShorthandCompact(config.GraphContext))
		} else {
			b.WriteString(BuildShorthand(config.GraphContext))
		}
	}

	if config.Level == LevelFull {
		if config.OutputSchema != "" {
			b.WriteString("\n")
			b.WriteString(config.OutputSchema)
		} else if config.TaskType != "" && config.TaskType != TaskGeneral {
			if schema := GetOutputSchema(config.TaskType); schema != nil {
				b.WriteString("\n")
				b.WriteString(FormatSchemaPrompt(schema))
			}
		}
	}

	b.WriteString("\n\nTASK:\n")
	b.WriteString(basePrompt)

	return b.String()
}

func EstimateTokenSavings(level CompressionLevel) float64 {
	switch level {
	case LevelFull:
		return 0.85
	case LevelCompact:
		return 0.75
	case LevelNormal:
		return 0.30
	default:
		return 0.0
	}
}
