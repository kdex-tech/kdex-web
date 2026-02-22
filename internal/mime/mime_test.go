package mime

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

type errReader struct{}

func (e errReader) Read(p []byte) (n int, err error) {
	return 0, errors.New("read error")
}

func TestDetect(t *testing.T) {
	tests := []struct {
		name        string
		input       io.Reader
		expectError bool
		expectMime  string
	}{
		{
			name:        "plain text",
			input:       strings.NewReader("hello world"),
			expectError: false,
			expectMime:  "text/plain; charset=utf-8",
		},
		{
			name:        "bufio text",
			input:       bufio.NewReader(strings.NewReader("hello world bufio")),
			expectError: false,
			expectMime:  "text/plain; charset=utf-8",
		},
		{
			name:        "error reader",
			input:       errReader{},
			expectError: true,
			expectMime:  "",
		},
		{
			name:        "json",
			input:       bytes.NewReader([]byte(`{"foo":"bar"}`)),
			expectError: false,
			expectMime:  "application/json",
		},
		{
			// Test when input length is exactly sniffLimit
			name:        "exact sniff limit",
			input:       bytes.NewReader(make([]byte, 3072)),
			expectError: false,
			expectMime:  "application/octet-stream",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mime, rc, err := Detect(tt.input)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error, got nil")
				}
				// BUG: Ensure failing tests log a review message
				if err == nil {
					t.Log("BUG: The reader should fail on errReader causing an io error during Peek, but it succeeded for some reason")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if mime == nil {
				t.Fatal("expected mime, got nil")
			}
			if mime.String() != tt.expectMime {
				t.Errorf("expected mime %s, got %s", tt.expectMime, mime.String())
				t.Logf("BUG: Mimetype detection failed. Expected %s, got %s. Mimetype detection library might have different outputs.", tt.expectMime, mime.String())
			}
			if rc == nil {
				t.Error("expected reader, got nil")
			}
		})
	}
}
