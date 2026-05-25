package scanner

import (
	"bytes"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/Universe/universe/internal/parser"
)

var skipDirs = map[string]struct{}{
	".git":         {},
	"vendor":       {},
	"node_modules": {},
	"__pycache__":  {},
	".venv":        {},
	"venv":         {},
	"env":          {},
	"dist":         {},
	"build":        {},
	"target":       {},
	".terraform":   {},
}

type ScannedFile struct {
	Path      string
	Extension string
	Language  string
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

func shouldSkipDir(name string) bool {
	if _, skip := skipDirs[name]; skip {
		return true
	}
	return isHiddenDirName(name)
}

func shouldSkipFilename(path string) bool {
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

// Scan walks the directory tree and returns all parseable source files
func (s *Scanner) Scan(rootPath string) ([]ScannedFile, error) {
	var out []ScannedFile

	root := filepath.Clean(rootPath)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			log.Printf("scanner: %s: %v", path, walkErr)
			return nil
		}
		if d.IsDir() {
			if filepath.Clean(path) == root {
				return nil
			}
			if shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldSkipFilename(path) {
			return nil
		}
		if fileLooksBinary(path) {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(path))
		p := s.registry.GetParser(ext)
		if p == nil {
			return nil
		}
		out = append(out, ScannedFile{
			Path:      path,
			Extension: ext,
			Language:  p.Language(),
		})
		return nil
	})
	return out, err
}
