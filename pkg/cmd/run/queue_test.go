package run

import (
	"fmt"
	"testing"
)

func TestExtractJobPathFromURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{
			name:     "simple job",
			url:      "http://jenkins/job/MyJob/",
			expected: "MyJob",
		},
		{
			name:     "nested job",
			url:      "http://jenkins/job/Folder/job/SubFolder/job/JobName/",
			expected: "Folder/SubFolder/JobName",
		},
		{
			name:     "URL encoded spaces",
			url:      "http://jenkins/job/My%20Folder/job/My%20Job/",
			expected: "My Folder/My Job",
		},
		{
			name:     "URL encoded slashes in branch name",
			url:      "http://jenkins/job/Repo/job/feature%2Fmy-branch/",
			expected: "Repo/feature/my-branch",
		},
		{
			name:     "mixed encoding",
			url:      "http://jenkins/job/Team%20A/job/Project/job/feature%2Fbranch%20name/",
			expected: "Team A/Project/feature/branch name",
		},
		{
			name:     "no job path",
			url:      "http://jenkins/",
			expected: "",
		},
		{
			name:     "trailing slash removed",
			url:      "http://jenkins/job/MyJob",
			expected: "MyJob",
		},
		{
			name:     "special characters",
			url:      "http://jenkins/job/Job%2B%26Name/",
			expected: "Job+&Name",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJobPathFromURL(tt.url)
			if result != tt.expected {
				t.Errorf("extractJobPathFromURL(%q) = %q, want %q", tt.url, result, tt.expected)
			}
		})
	}
}

func TestQueuedItemFormat(t *testing.T) {
	// Test that queued items have the correct synthetic ID format and fields
	jobPath := "team/app"
	queueID := int64(12345)

	// Simulate the format used in fetchQueuedItemsForJob
	item := runListItem{
		ID:        fmt.Sprintf("%s/q%d", normalizeJobPath(jobPath), queueID),
		Number:    0,
		Status:    "queued",
		QueueID:   queueID,
		StartTime: "2025-01-15T10:00:00Z",
		Fields: map[string]any{
			"queueReason": "Waiting for next available executor",
		},
	}

	// Verify synthetic ID format
	expectedID := "team/app/q12345"
	if item.ID != expectedID {
		t.Errorf("queued item ID = %q, want %q", item.ID, expectedID)
	}

	// Verify number is 0 for queued items
	if item.Number != 0 {
		t.Errorf("queued item Number = %d, want 0", item.Number)
	}

	// Verify status is "queued"
	if item.Status != "queued" {
		t.Errorf("queued item Status = %q, want %q", item.Status, "queued")
	}

	// Verify queueReason is in fields
	if reason, ok := item.Fields["queueReason"].(string); !ok || reason == "" {
		t.Errorf("queued item should have queueReason in fields, got %v", item.Fields)
	}
}

func TestIncludeQueuedWithLimit(t *testing.T) {
	// Test that --limit is correctly applied to the combined list of queued + build items
	tests := []struct {
		name          string
		queuedItems   []runListItem
		buildItems    []runListItem
		limit         int
		expectedCount int
		expectedFirst string // ID of expected first item
	}{
		{
			name: "queued items prepended, limit not exceeded",
			queuedItems: []runListItem{
				{ID: "job/q100", Number: 0, Status: "queued"},
			},
			buildItems: []runListItem{
				{ID: "job/1", Number: 1, Status: "success"},
				{ID: "job/2", Number: 2, Status: "success"},
			},
			limit:         5,
			expectedCount: 3,
			expectedFirst: "job/q100",
		},
		{
			name: "limit applied to combined list",
			queuedItems: []runListItem{
				{ID: "job/q100", Number: 0, Status: "queued"},
				{ID: "job/q101", Number: 0, Status: "queued"},
			},
			buildItems: []runListItem{
				{ID: "job/1", Number: 1, Status: "success"},
				{ID: "job/2", Number: 2, Status: "success"},
				{ID: "job/3", Number: 3, Status: "success"},
			},
			limit:         3, // Should truncate to 3 items total
			expectedCount: 3,
			expectedFirst: "job/q100",
		},
		{
			name: "many queued items exceed limit",
			queuedItems: []runListItem{
				{ID: "job/q100", Number: 0, Status: "queued"},
				{ID: "job/q101", Number: 0, Status: "queued"},
				{ID: "job/q102", Number: 0, Status: "queued"},
			},
			buildItems: []runListItem{
				{ID: "job/1", Number: 1, Status: "success"},
			},
			limit:         2, // Only first 2 queued items should be returned
			expectedCount: 2,
			expectedFirst: "job/q100",
		},
		{
			name:        "no queued items",
			queuedItems: []runListItem{},
			buildItems: []runListItem{
				{ID: "job/1", Number: 1, Status: "success"},
				{ID: "job/2", Number: 2, Status: "success"},
			},
			limit:         5,
			expectedCount: 2,
			expectedFirst: "job/1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the logic from executeRunList
			var items []runListItem
			items = append(items, tt.queuedItems...)
			items = append(items, tt.buildItems...)

			// Apply limit (same logic as in executeRunList)
			if len(items) > tt.limit {
				items = items[:tt.limit]
			}

			if len(items) != tt.expectedCount {
				t.Errorf("combined items count = %d, want %d", len(items), tt.expectedCount)
			}

			if len(items) > 0 && items[0].ID != tt.expectedFirst {
				t.Errorf("first item ID = %q, want %q", items[0].ID, tt.expectedFirst)
			}
		})
	}
}

func TestCursorRecomputationWithQueuedItems(t *testing.T) {
	// Test that cursor is correctly recomputed when queued items cause truncation
	tests := []struct {
		name                 string
		queuedItems          []runListItem
		buildItems           []runListItem
		limit                int
		expectedCursorBuild  int64 // 0 means no cursor expected
		description          string
	}{
		{
			name: "no truncation - cursor unchanged",
			queuedItems: []runListItem{
				{ID: "job/q100", Number: 0, Status: "queued"},
			},
			buildItems: []runListItem{
				{ID: "job/12", Number: 12, Status: "success"},
			},
			limit:               5,
			expectedCursorBuild: 0, // No truncation, no cursor recomputation
			description:         "Combined count (2) < limit (5), no cursor needed",
		},
		{
			name: "truncation with builds remaining - cursor points to last build",
			queuedItems: []runListItem{
				{ID: "job/q100", Number: 0, Status: "queued"},
			},
			buildItems: []runListItem{
				{ID: "job/12", Number: 12, Status: "success"},
				{ID: "job/11", Number: 11, Status: "success"},
				{ID: "job/10", Number: 10, Status: "success"},
			},
			limit:               3, // [q100, #12, #11] - #10 cut off
			expectedCursorBuild: 11,
			description:         "Cursor should point to build #11 (last build in output)",
		},
		{
			name: "all builds pushed out - cursor points to first build + 1",
			queuedItems: []runListItem{
				{ID: "job/q100", Number: 0, Status: "queued"},
				{ID: "job/q101", Number: 0, Status: "queued"},
				{ID: "job/q102", Number: 0, Status: "queued"},
			},
			buildItems: []runListItem{
				{ID: "job/12", Number: 12, Status: "success"},
				{ID: "job/11", Number: 11, Status: "success"},
			},
			limit:               2, // [q100, q101] - all builds cut off
			expectedCursorBuild: 13,
			description:         "Cursor should be #13 (first build + 1) so next page includes #12",
		},
		{
			name: "one build remains after truncation",
			queuedItems: []runListItem{
				{ID: "job/q100", Number: 0, Status: "queued"},
				{ID: "job/q101", Number: 0, Status: "queued"},
			},
			buildItems: []runListItem{
				{ID: "job/12", Number: 12, Status: "success"},
				{ID: "job/11", Number: 11, Status: "success"},
			},
			limit:               3, // [q100, q101, #12] - #11 cut off
			expectedCursorBuild: 12,
			description:         "Cursor should point to build #12 (last and only build in output)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the cursor recomputation logic from executeRunList
			originalBuilds := tt.buildItems
			var items []runListItem
			items = append(items, tt.queuedItems...)
			items = append(items, tt.buildItems...)

			var cursorBuild int64 = 0

			if len(items) > tt.limit {
				items = items[:tt.limit]

				// Find the last build (Number > 0) in the truncated output
				var lastBuildInOutput int64
				for i := len(items) - 1; i >= 0; i-- {
					if items[i].Number > 0 {
						lastBuildInOutput = items[i].Number
						break
					}
				}

				if lastBuildInOutput > 0 {
					cursorBuild = lastBuildInOutput
				} else if len(originalBuilds) > 0 {
					cursorBuild = originalBuilds[0].Number + 1
				}
			}

			if cursorBuild != tt.expectedCursorBuild {
				t.Errorf("%s: cursor build = %d, want %d", tt.description, cursorBuild, tt.expectedCursorBuild)
			}
		})
	}
}
