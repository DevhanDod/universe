package scanner

import (
	"bufio"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// gitignoreRule is a single non-comment line from a .gitignore.
// Stored relative to the .gitignore file that contained it — match() rebases
// candidate paths against that directory before checking the pattern.
type gitignoreRule struct {
	source   string // "<repo-relative path>:<line number>"
	baseDir  string // absolute dir the .gitignore lived in
	pattern  string // cleaned pattern (no leading !, no trailing comments)
	negate   bool   // pattern starts with `!`
	dirOnly  bool   // pattern ends with `/`
	rooted   bool   // pattern has an embedded `/` or leading `/` — anchored to baseDir
}

type gitignoreSet struct {
	rules []gitignoreRule
}

// loadGitignores finds every .gitignore beneath root and loads its rules.
// We do NOT honor git's "rules nested deeper override shallower" semantics
// perfectly — we just append in walk order and the last matching rule wins.
// That's a small fidelity gap vs real git, but fine for "is this file
// expected to be tracked".
func loadGitignores(root string) *gitignoreSet {
	set := &gitignoreSet{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d == nil {
			return nil
		}
		if d.IsDir() {
			if path != root && shouldHardSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		base := filepath.Base(path)
		if base != ".gitignore" {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return nil
		}
		defer f.Close()

		dir := filepath.Dir(path)
		rel, _ := filepath.Rel(root, path)
		sc := bufio.NewScanner(f)
		lineNo := 0
		for sc.Scan() {
			lineNo++
			line := strings.TrimRight(sc.Text(), " \t\r")
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			negate := false
			if strings.HasPrefix(line, "!") {
				negate = true
				line = line[1:]
			}
			dirOnly := false
			if strings.HasSuffix(line, "/") {
				dirOnly = true
				line = strings.TrimSuffix(line, "/")
			}
			rooted := strings.HasPrefix(line, "/") || strings.Contains(line, "/")
			line = strings.TrimPrefix(line, "/")

			set.rules = append(set.rules, gitignoreRule{
				source:  fmt.Sprintf("%s:%d", filepath.ToSlash(rel), lineNo),
				baseDir: dir,
				pattern: line,
				negate:  negate,
				dirOnly: dirOnly,
				rooted:  rooted,
			})
		}
		return nil
	})
	return set
}

// match returns (ignored, source). Walks rules in order; last match wins so
// later `!unignore` rules can override earlier ignores, mirroring git.
func (s *gitignoreSet) match(absPath, root string) (bool, string) {
	if s == nil || len(s.rules) == 0 {
		return false, ""
	}
	ignored := false
	source := ""
	info, err := os.Stat(absPath)
	isDir := err == nil && info.IsDir()

	for _, r := range s.rules {
		if r.dirOnly && !isDir {
			continue
		}
		rel, err := filepath.Rel(r.baseDir, absPath)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		rel = filepath.ToSlash(rel)

		if matchGitignorePattern(r.pattern, rel, r.rooted) {
			ignored = !r.negate
			if ignored {
				source = r.source
			} else {
				source = ""
			}
		}
	}
	return ignored, source
}

// matchGitignorePattern implements a small subset of gitignore globbing:
//   - `*` matches any run of non-slash chars
//   - `?` matches one non-slash char
//   - `**` matches across slashes
//   - rooted patterns are matched against `path`; un-rooted match any
//     trailing path component
//
// Character classes (`[abc]`) are not supported — they're rare in real
// .gitignores and adding them isn't worth the complexity here.
func matchGitignorePattern(pattern, path string, rooted bool) bool {
	if pattern == "" {
		return false
	}
	if rooted {
		return globMatch(pattern, path)
	}
	// un-rooted: pattern can match basename OR any directory along the path
	if globMatch(pattern, filepath.Base(path)) {
		return true
	}
	parts := strings.Split(path, "/")
	for i := range parts {
		if globMatch(pattern, strings.Join(parts[i:], "/")) {
			return true
		}
	}
	return false
}

// globMatch supports *, ?, and ** with `/` handled specially.
func globMatch(pattern, name string) bool {
	return doGlob([]byte(pattern), []byte(name))
}

func doGlob(pat, name []byte) bool {
	pi, ni := 0, 0
	starPi, starNi := -1, 0
	for ni < len(name) {
		switch {
		case pi < len(pat) && pat[pi] == '*':
			if pi+1 < len(pat) && pat[pi+1] == '*' {
				// ** consumes across slashes
				pi += 2
				if pi == len(pat) {
					return true
				}
				// skip an optional slash right after **
				if pat[pi] == '/' {
					pi++
				}
				starPi = -1 // disable single-* backtracking past **
				// try every position in `name` from here onward
				for j := ni; j <= len(name); j++ {
					if doGlob(pat[pi:], name[j:]) {
						return true
					}
				}
				return false
			}
			// single * — does NOT cross slash
			starPi = pi
			starNi = ni
			pi++
		case pi < len(pat) && (pat[pi] == '?' || pat[pi] == name[ni]):
			if pat[pi] == '?' && name[ni] == '/' {
				// ? must not match slash
				if starPi >= 0 {
					pi = starPi + 1
					starNi++
					ni = starNi
					continue
				}
				return false
			}
			pi++
			ni++
		case starPi >= 0:
			if name[starNi] == '/' {
				return false // single * can't cross slash
			}
			pi = starPi + 1
			starNi++
			ni = starNi
		default:
			return false
		}
	}
	for pi < len(pat) && pat[pi] == '*' {
		pi++
	}
	return pi == len(pat)
}
