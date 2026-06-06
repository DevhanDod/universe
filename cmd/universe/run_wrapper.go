package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// universe init writes a small wrapper script into the project's
// .universe/ directory and the generated Cursor rule points the agent
// at it via a relative path. This solves two problems we hit when the
// rule referenced the binary directly:
//
//   1. Cursor's Shell tool launches with a clean environment; on
//      Windows the npm global bin directory (.../AppData/Roaming/npm)
//      is typically not on PATH, so `universe query X` resolved to
//      CommandNotFoundException.
//   2. Embedding the absolute install path inside the rule meant the
//      rule was machine-specific — copying a checkout to another
//      machine, or reinstalling universe to a different directory,
//      silently broke every Shell call the agent made.
//
// A relative wrapper file (.universe/run.cmd on Windows,
// .universe/run.sh on POSIX) flips both: the rule contains the same
// string on every machine, and `universe init` rewrites the wrapper
// with the freshly-resolved absolute path each time it runs.

// universeRunWrapperFile returns the wrapper filename appropriate for
// the host OS. We keep both names in one place so the rule generator
// can reference the same value the writer produces.
func universeRunWrapperFile() string {
	if runtime.GOOS == "windows" {
		return "run.cmd"
	}
	return "run.sh"
}

// universeRunWrapperRelPath is what the generated rule shows the
// agent. Forward-slashed so it reads the same in markdown on every
// platform; both Windows .cmd and POSIX shells accept forward slashes
// in invocations.
func universeRunWrapperRelPath() string {
	return ".universe/" + universeRunWrapperFile()
}

// writeUniverseRunWrapper writes the per-project wrapper script. It
// resolves the absolute path of the currently-running binary, embeds
// it in the wrapper, and overwrites any existing wrapper — unlike the
// rule and hooks files, we treat this one as fully owned by us
// because regenerating it on every init is the whole point.
//
// Returns (path, err). Errors are surfaced so init can warn the
// developer; a missing wrapper means the rule's relative path won't
// work, which we'd rather flag loudly than hide.
func writeUniverseRunWrapper(projectDir string) (string, error) {
	dir := filepath.Join(projectDir, ".universe")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	bin := universeBinaryPath()
	path := filepath.Join(dir, universeRunWrapperFile())

	var body string
	if runtime.GOOS == "windows" {
		// Quoting: surrounding double quotes preserve spaces in the
		// path; %* passes through every argument the caller supplied.
		// @echo off keeps the wrapper invocation invisible in transcripts.
		body = fmt.Sprintf("@echo off\r\n\"%s\" %%*\r\n", bin)
	} else {
		// exec replaces the wrapper shell process so exit codes and
		// signal handling propagate as if the agent had called the
		// binary directly. The shebang stays portable across distros.
		body = fmt.Sprintf("#!/bin/sh\nexec \"%s\" \"$@\"\n", bin)
	}

	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		return path, err
	}
	return path, nil
}
