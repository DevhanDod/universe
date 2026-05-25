//go:build !cgo

package parser

import (
	"errors"

	"github.com/Universe/universe/internal/models"
)

type PythonParser struct{}

func NewPythonParser() *PythonParser {
	return &PythonParser{}
}

func (*PythonParser) Language() string { return "python" }

func (*PythonParser) SupportedExtensions() []string { return []string{".py"} }

func (*PythonParser) Parse(_ string, _ []byte) (*models.ParseResult, error) {
	return nil, errors.New("universe: Python parsing requires CGO and a C compiler (e.g. gcc); enable CGO and install a toolchain, then rebuild")
}

var _ Parser = (*PythonParser)(nil)
