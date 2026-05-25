package extractor

import "github.com/Universe/universe/internal/models"

// Extractor adds semantic analysis on top of parsed results
type Extractor interface {
	Extract(result *models.ParseResult, allResults []*models.ParseResult) (*models.ParseResult, error)
	Language() string
}
