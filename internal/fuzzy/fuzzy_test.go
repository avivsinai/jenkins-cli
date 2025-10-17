package fuzzy

import (
	"testing"
)

func TestSearch(t *testing.T) {
	items := []string{
		"Tools/ada/master",
		"Tools/ada/PR-22",
		"Tools/ada/feature/slack-history-qa",
		"LLM Platform",
		"ada-service",
		"Build.Ada.Artifacts",
		"Deploy.Ada.Production",
		"Tools/other-service/master",
	}

	tests := []struct {
		name         string
		query        string
		items        []string
		maxResults   int
		wantContains []string
		wantFirst    string
	}{
		{
			name:         "exact match",
			query:        "Tools/ada/master",
			items:        items,
			maxResults:   5,
			wantContains: []string{"Tools/ada/master"},
			wantFirst:    "Tools/ada/master",
		},
		{
			name:         "partial path match",
			query:        "ada",
			items:        items,
			maxResults:   5,
			wantContains: []string{"Tools/ada/master", "ada-service", "Build.Ada.Artifacts"},
			wantFirst:    "ada-service",
		},
		{
			name:         "path component match",
			query:        "Tools/ada",
			items:        items,
			maxResults:   5,
			wantContains: []string{"Tools/ada/master", "Tools/ada/PR-22"},
			wantFirst:    "Tools/ada/master",
		},
		{
			name:         "case insensitive",
			query:        "ADA",
			items:        items,
			maxResults:   5,
			wantContains: []string{"Tools/ada/master", "ada-service"},
		},
		{
			name:         "max results limit",
			query:        "ada",
			items:        items,
			maxResults:   2,
			wantContains: []string{"ada-service", "Tools/ada/master"},
		},
		{
			name:       "no matches",
			query:      "nonexistent",
			items:      items,
			maxResults: 5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := Search(tt.query, tt.items, tt.maxResults)

			if len(tt.wantContains) == 0 {
				if len(matches) != 0 {
					t.Errorf("Search() expected no matches, got %d", len(matches))
				}
				return
			}

			if tt.maxResults > 0 && len(matches) > tt.maxResults {
				t.Errorf("Search() returned %d matches, expected max %d", len(matches), tt.maxResults)
			}

			if tt.wantFirst != "" && len(matches) > 0 {
				if matches[0].Value != tt.wantFirst {
					t.Errorf("Search() first match = %v, want %v", matches[0].Value, tt.wantFirst)
				}
			}

			for _, want := range tt.wantContains {
				found := false
				for _, match := range matches {
					if match.Value == want {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Search() expected to contain %v, but it was not found in results", want)
				}
			}
		})
	}
}

func TestCalculateScore(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		target      string
		expectScore bool // true if we expect score > 0
	}{
		{
			name:        "exact match",
			query:       "test",
			target:      "test",
			expectScore: true,
		},
		{
			name:        "substring match",
			query:       "ada",
			target:      "Tools/ada/master",
			expectScore: true,
		},
		{
			name:        "case insensitive",
			query:       "ADA",
			target:      "ada",
			expectScore: true,
		},
		{
			name:        "no match",
			query:       "xyz",
			target:      "abc",
			expectScore: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := calculateScore(tt.query, tt.target)
			hasScore := score > 0

			if hasScore != tt.expectScore {
				t.Errorf("calculateScore() = %v, expectScore %v", score, tt.expectScore)
			}
		})
	}
}
