package utils_test

import (
	"testing"

	"github.com/kdex-tech/kdex-host/internal/utils"
	. "github.com/onsi/gomega"
)

func TestDomainsToMatcher(t *testing.T) {
	tests := []struct {
		name string // description of this test case
		// Named input parameters for target function.
		domains []string
		want    string
	}{
		{
			name:    "single simple domain",
			domains: []string{"foo.bar"},
			want:    "foo\\.bar",
		},
		{
			name:    "single wildcard domain",
			domains: []string{"*.foo.bar"},
			want:    ".*\\.foo\\.bar",
		},
		{
			name:    "multiple simple domains",
			domains: []string{"foo.bar", "fiz.bum"},
			want:    "foo\\.bar|fiz\\.bum",
		},
		{
			name:    "multiple wildcard domains",
			domains: []string{"*.foo.bar", "*.fiz.bum"},
			want:    ".*\\.foo\\.bar|.*\\.fiz\\.bum",
		},
		{
			name:    "multiple mixed domains",
			domains: []string{"foo.bar", "*.fiz.bum"},
			want:    "foo\\.bar|.*\\.fiz\\.bum",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewGomegaWithT(t)

			got := utils.DomainsToMatcher(tt.domains)
			g.Expect(got).To(Equal(tt.want))
		})
	}
}
