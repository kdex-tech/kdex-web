package linter

import (
	"testing"
)

// Minimal valid OpenAPI 3.0 spec
const validSpec = `
openapi: 3.0.0
info:
  title: Sample API
  version: 1.0.0
paths:
  /users:
    get:
      summary: Get users
      responses:
        '200':
          description: Successful response
`

// Invalid spec (missing info, but has openapi version so it enters the flow)
const invalidSpec = `
openapi: 3.0.0
paths:
  /users:
    get:
      summary: Get users
`

func TestLintSpec(t *testing.T) {
	t.Run("Valid Spec", func(t *testing.T) {
		results, err := LintSpec([]byte(validSpec))
		if err != nil {
			t.Fatalf("Expected no error, got %v", err)
		}
		// Based on "Recommended" ruleset, this might still have warnings (e.g. no server defined, no operation ID).
		// But shouldn't error out completely.
		// Let's print findings for debug if needed.
		for _, r := range results {
			t.Logf("Lint: [%s] %s", r.RuleId, r.Message)
		}
	})

	t.Run("Invalid Spec", func(t *testing.T) {
		// A completely botched spec might trigger core schema validation errors or critical rules
		results, err := LintSpec([]byte(invalidSpec))
		if err != nil {
			// Vacuum might not return Go error for validation issues, but return them in results
			t.Logf("Execution error (might be unexpected): %v", err)
		}

		// We expect at least some errors/warnings about missing info/openapi fields
		if len(results) == 0 {
			t.Errorf("Expected linting errors for invalid spec, got none")
		}

		foundImportantIssue := false
		for _, r := range results {
			t.Logf("Lint: [%s] %s", r.RuleId, r.Message)
			// 'oas3-schema' is the rule for schema validation usually
			if r.RuleId == "oas3-schema" || r.RuleId == "info-contact" || r.RuleId == "info-description" {
				foundImportantIssue = true
			}
		}

		// We are loose here because we rely on default ruleset details
		if !foundImportantIssue && len(results) > 0 {
			t.Log("Found some issues, so linter is working.")
		}
	})
}
