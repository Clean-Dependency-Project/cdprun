package jetbrains

import (
	"strings"
	"testing"
)

func TestParseChecksumLine(t *testing.T) {
	line := strings.Repeat("ab", 32) + " *file.tar.gz"
	got, err := ParseChecksumLine(line)
	if err != nil {
		t.Fatalf("ParseChecksumLine: %v", err)
	}
	want := strings.Repeat("ab", 32)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestParseChecksumLine_InvalidLength(t *testing.T) {
	_, err := ParseChecksumLine("abcd *file.tar.gz")
	if err == nil {
		t.Fatal("expected error for invalid checksum length")
	}
}
