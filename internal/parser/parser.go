package parser

import "github.com/Universe/universe/internal/models"

// Parser interface — every language parser implements this
type Parser interface {
	Parse(filePath string, content []byte) (*models.ParseResult, error)
	SupportedExtensions() []string
	Language() string
}
