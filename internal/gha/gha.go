// Package gha implements the small subset of the GitHub Actions runner
// protocol (workflow commands and step outputs) that ego needs.
// Forgejo/Gitea Actions runners speak the same protocol.
package gha

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"strings"
)

// Logger writes workflow commands when running under Actions and plain
// prefixed lines otherwise (e.g. when the CLI is run locally).
type Logger struct {
	W       io.Writer
	Actions bool
}

// NewLogger returns a Logger on stderr that emits workflow commands when
// GITHUB_ACTIONS=true.
func NewLogger() *Logger {
	return &Logger{W: os.Stderr, Actions: os.Getenv("GITHUB_ACTIONS") == "true"}
}

// Errorf writes an error annotation.
func (l *Logger) Errorf(format string, args ...any) {
	l.command("error", fmt.Sprintf(format, args...))
}

// Warningf writes a warning annotation.
func (l *Logger) Warningf(format string, args ...any) {
	l.command("warning", fmt.Sprintf(format, args...))
}

// Infof writes a plain log line.
func (l *Logger) Infof(format string, args ...any) {
	fmt.Fprintf(l.W, format+"\n", args...)
}

func (l *Logger) command(name, msg string) {
	if l.Actions {
		fmt.Fprintf(l.W, "::%s::%s\n", name, escapeData(msg))
		return
	}
	fmt.Fprintf(l.W, "%s: %s\n", name, msg)
}

// escapeData escapes a workflow command message so embedded newlines or
// '%' cannot terminate the command or inject a new one.
func escapeData(s string) string {
	return strings.NewReplacer("%", "%25", "\r", "%0D", "\n", "%0A").Replace(s)
}

// SetOutput appends name=value to the step output file at path using the
// multiline heredoc form. The delimiter is random so that value (which may be
// LLM output derived from untrusted PR content) cannot close the block early
// and forge additional outputs.
func SetOutput(path, name, value string) error {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Errorf("generate output delimiter: %w", err)
	}
	delim := "EGO_EOF_" + hex.EncodeToString(buf)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o600)
	if err != nil {
		return fmt.Errorf("open step output file: %w", err)
	}
	_, werr := fmt.Fprintf(f, "%s<<%s\n%s\n%s\n", name, delim, value, delim)
	cerr := f.Close()
	if werr != nil {
		return fmt.Errorf("write step output: %w", werr)
	}
	return cerr
}
