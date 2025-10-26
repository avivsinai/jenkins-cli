package build

import (
	"strings"
	"testing"
)

func TestVersion(t *testing.T) {
	// Save original values
	origVersion := versionFromLdflags
	origCommit := commitFromLdflags
	origDate := dateFromLdflags

	// Restore after test
	defer func() {
		versionFromLdflags = origVersion
		commitFromLdflags = origCommit
		dateFromLdflags = origDate
	}()

	tests := []struct {
		name           string
		ldflags        string
		expectContains string
		expectNotDev   bool
	}{
		{
			name:           "ldflags set to version",
			ldflags:        "1.2.3",
			expectContains: "1.2.3",
			expectNotDev:   true,
		},
		{
			name:           "ldflags set to dev",
			ldflags:        "dev",
			expectContains: "", // could be dev or module version
			expectNotDev:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			versionFromLdflags = tt.ldflags
			v := version()

			if tt.expectNotDev && v == "dev" {
				t.Errorf("expected version to not be 'dev', got %q", v)
			}

			if tt.expectContains != "" && !strings.Contains(v, tt.expectContains) {
				t.Errorf("expected version to contain %q, got %q", tt.expectContains, v)
			}
		})
	}
}

func TestCommit(t *testing.T) {
	// Save original values
	origCommit := commitFromLdflags

	// Restore after test
	defer func() {
		commitFromLdflags = origCommit
	}()

	// Test with ldflags set
	commitFromLdflags = "abc123def456"
	c := commit()
	if c != "abc123def456" {
		t.Errorf("expected commit %q, got %q", "abc123def456", c)
	}

	// Test with empty ldflags - will fall back to runtime/debug or empty
	commitFromLdflags = ""
	c = commit()
	// We can't predict what runtime/debug will return, so just check it doesn't panic
	_ = c
}
