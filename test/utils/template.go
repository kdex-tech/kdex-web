package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTemplate(t *testing.T) {
	type testargs struct {
	}

	// This is what I am testing
	type want struct{}
	funcUnderTest := func(_ testargs) (want, error) {
		return want{}, nil
	}

	tests := []struct {
		name       string
		args       testargs
		assertions func(t *testing.T, got want, goterr error)
	}{
		{
			name: "basic",
			args: testargs{},
			assertions: func(t *testing.T, got want, gotErr error) {
				assert.Equal(t, want{}, got)
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, gotErr := funcUnderTest(tt.args)
			tt.assertions(t, got, gotErr)
		})
	}
}
