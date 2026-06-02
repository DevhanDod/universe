package scanner

import (
	"bytes"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/Universe/universe/internal/models"
	"github.com/Universe/universe/internal/parser"
)

// hardSkipDirs are directories we never descend into regardless of gitignore.
// .git itself is internal storage (not user content); the others are caches
// that would balloon scan time without giving the agent anything useful.
var hardSkipDirs = map[string]struct{}{
	".git":        {},
	"node_modules": {},
	"__pycache__":  {},
	".venv":        {},
	"venv":         {},
}

// ScannedFile describes every file the scanner emits — including ones with no
// parser and ones matched by .gitignore. Tags let the analyzer/agent decide
// what to do rather than the scanner silently dropping them.
type ScannedFile struct {
	Path       string
	Extension  string
	Language   string // "" when no parser claims this extension
	Kind       string // models.FileKind*
	Size       int64
	IsBinary   bool
	IsIgnored  bool   // matched a .gitignore pattern
	IgnoredBy  string // "<gitignore file>:<line>" or pattern text
	IsGenerated bool  // *.pb.go, *_generated.go, …
}

type Scanner struct {
	registry *parser.Registry
}

func NewScanner(registry *parser.Registry) *Scanner {
	return &Scanner{registry: registry}
}

func isHiddenDirName(name string) bool {
	switch name {
	case ".", "..":
		return false
	default:
		return strings.HasPrefix(name, ".")
	}
}

func shouldHardSkipDir(name string) bool {
	if _, skip := hardSkipDirs[name]; skip {
		return true
	}
	// hidden dirs (.idea, .vscode, .terraform, …) — still skipped from descent
	// because they're rarely useful and explode the file count. Users who need
	// them can drop the prefix.
	return isHiddenDirName(name)
}

func isGeneratedFilename(path string) bool {
	base := filepath.Base(path)
	if strings.HasSuffix(base, ".pb.go") {
		return true
	}
	if strings.HasSuffix(base, "_generated.go") {
		return true
	}
	return false
}

func fileLooksBinary(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		log.Printf("scanner: open %s: %v", path, err)
		return true
	}
	defer func() {
		if err := f.Close(); err != nil {
			log.Printf("scanner: close %s: %v", path, err)
		}
	}()

	buf := make([]byte, 8192)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		log.Printf("scanner: read %s: %v", path, err)
		return true
	}
	return bytes.IndexByte(buf[:n], 0) >= 0
}

// classifyFile picks a FileKind from extension + name. Used for files with no
// structural parser so the graph still knows what they are.
func classifyFile(path string, isBinary bool) string {
	base := strings.ToLower(filepath.Base(path))
	ext := strings.ToLower(filepath.Ext(path))

	switch ext {
	case ".md", ".rst", ".txt", ".adoc":
		return models.FileKindDoc
	case ".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".webp", ".bmp":
		return models.FileKindImage
	case ".yaml", ".yml", ".toml", ".ini", ".cfg", ".conf", ".env",
		".json", ".xml", ".properties", ".tf", ".tfvars":
		return models.FileKindConfig
	case ".lock":
		return models.FileKindLockfile
	}
	switch base {
	case "dockerfile", "makefile", "rakefile", "gemfile", "procfile", ".gitignore",
		".dockerignore", ".editorconfig", ".gitattributes":
		return models.FileKindConfig
	case "go.sum", "package-lock.json", "yarn.lock", "poetry.lock", "pnpm-lock.yaml", "cargo.lock":
		return models.FileKindLockfile
	}
	if isBinary {
		return models.FileKindBinary
	}
	return models.FileKindOther
}

// Scan walks the directory tree and returns every file (parseable or not),
// tagged with kind / ignored / binary status. The analyzer decides what to do
// with each tag; the scanner no longer silently drops files.
func (s *Scanner) Scan(rootPath string) ([]ScannedFile, error) {
	var out []ScannedFile

	root := filepath.Clean(rootPath)
	ignore := loadGitignores(root)

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			log.Printf("scanner: %s: %v", path, walkErr)
			return nil
		}
		if d.IsDir() {
			if filepath.Clean(path) == root {
				return nil
			}
			if shouldHardSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		var size int64
		if err == nil {
			size = info.Size()
		}

		isBinary := fileLooksBinary(path)
		ext := strings.ToLower(filepath.Ext(path))
		p := s.registry.GetParser(ext)

		kind := models.FileKindOther
		lang := ""
		if p != nil && !isBinary {
			kind = models.FileKindSource
			lang = p.Language()
		} else {
			kind = classifyFile(path, isBinary)
		}

		ignored, ignoredBy := ignore.match(path, root)

		out = append(out, ScannedFile{
			Path:        path,
			Extension:   ext,
			Language:    lang,
			Kind:        kind,
			Size:        size,
			IsBinary:    isBinary,
			IsIgnored:   ignored,
			IgnoredBy:   ignoredBy,
			IsGenerated: isGeneratedFilename(path),
		})
		return nil
	})
	return out, err
}
