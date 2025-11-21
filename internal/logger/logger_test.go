package logger

import (
	"testing"
)

func TestNewLogger_ValidInputs(t *testing.T) {
	cases := []struct {
		level  string
		format string
	}{
		{"info", "json"},
		{"debug", "text"},
		{"warn", "json"},
		{"error", "text"},
	}
	for _, c := range cases {
		l, err := New(c.level, c.format)
		if err != nil {
			t.Errorf("expected no error for level=%q format=%q, got %v", c.level, c.format, err)
		}
		if l == nil {
			t.Errorf("expected logger for level=%q format=%q, got nil", c.level, c.format)
		}
	}
}

func TestNewLogger_EmptyStrings(t *testing.T) {
	_, err := New("", "json")
	if err == nil {
		t.Error("expected error for empty logLevel, got nil")
	}
	_, err = New("info", "")
	if err == nil {
		t.Error("expected error for empty logFormat, got nil")
	}
	_, err = New("", "")
	if err == nil {
		t.Error("expected error for both logLevel and logFormat empty, got nil")
	}
}

func TestNewLogger_InvalidValues(t *testing.T) {
	_, err := New("foo", "json")
	if err == nil {
		t.Error("expected error for invalid logLevel, got nil")
	}
	_, err = New("info", "bar")
	if err == nil {
		t.Error("expected error for invalid logFormat, got nil")
	}
}
