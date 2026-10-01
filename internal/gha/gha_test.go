package gha

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestSetOutputUsesRandomDelimiter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "output")
	// A value that tries to close a predictable heredoc and forge an output.
	value := "looks fine\nEOF\nevil=1\n"
	if err := SetOutput(path, "review", value); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`(?s)^review<<(AI_REVIEWER_EOF_[0-9a-f]{32})\n(.*)\n(AI_REVIEWER_EOF_[0-9a-f]{32})\n$`).FindSubmatch(got)
	if m == nil {
		t.Fatalf("unexpected output file:\n%s", got)
	}
	if !bytes.Equal(m[1], m[3]) {
		t.Errorf("opening delimiter %s != closing %s", m[1], m[3])
	}
	if string(m[2]) != value {
		t.Errorf("value = %q, want %q", m[2], value)
	}
}

func TestLoggerEscapesWorkflowCommands(t *testing.T) {
	var buf bytes.Buffer
	l := &Logger{W: &buf, Actions: true}
	l.Errorf("bad 100%%\n::warning::injected")
	if got, want := buf.String(), "::error::bad 100%25%0A::warning::injected\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	buf.Reset()
	l.Actions = false
	l.Warningf("plain")
	if got, want := buf.String(), "warning: plain\n"; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
