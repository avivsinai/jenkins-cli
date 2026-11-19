package run

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/avivsinai/jenkins-cli/internal/config"
	"github.com/avivsinai/jenkins-cli/internal/jenkins"
)

func TestExecuteRunListPagesThroughHistory(t *testing.T) {
	for _, tt := range []struct {
		name  string
		limit int
		gaps  bool
	}{
		{name: "limit above 100", limit: 300},
		{name: "limit spans API ranges", limit: 650},
		{name: "small pages past 100", limit: 20},
		{name: "deleted builds", limit: 100, gaps: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var history []runSummary
			for n := int64(873); n > 0; n-- {
				if tt.gaps && n%7 == 0 {
					continue
				}
				history = append(history, runSummary{Number: n, Result: "SUCCESS"})
			}

			// Jenkins caps builds before applying the tree range; allBuilds
			// exposes the full retained history under its own JSON key.
			treePattern := regexp.MustCompile(`^(builds|allBuilds)\[.*\]\{(\d*),(\d+)\}$`)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/job/app/api/json", r.URL.Path)
				match := treePattern.FindStringSubmatch(r.URL.Query().Get("tree"))
				if len(match) != 4 {
					t.Errorf("unexpected tree query: %s", r.URL.RawQuery)
					http.Error(w, "invalid tree", http.StatusBadRequest)
					return
				}
				start := 0
				if match[2] != "" {
					var err error
					start, err = strconv.Atoi(match[2])
					require.NoError(t, err)
				}
				end, err := strconv.Atoi(match[3])
				require.NoError(t, err)
				builds := history
				if match[1] == "builds" {
					builds = builds[:min(100, len(builds))]
				}
				start = min(start, len(builds))
				end = min(end, len(builds))
				w.Header().Set("Content-Type", "application/json")
				require.NoError(t, json.NewEncoder(w).Encode(map[string]any{match[1]: builds[start:end]}))
			}))
			t.Cleanup(server.Close)

			t.Setenv("JK_TOKEN", "test-token")
			cfg := &config.Config{}
			cfg.SetContext("test", &config.Context{URL: server.URL, Username: "test"})
			client, err := jenkins.NewClient(context.Background(), cfg, "test", jenkins.WithSkipCapabilityProbe(), jenkins.WithDisableWarn(true))
			require.NoError(t, err)

			cursor := ""
			for offset := 0; offset < len(history); offset += tt.limit {
				out, err := executeRunList(context.Background(), client, "app", runListOptions{Limit: tt.limit, Cursor: cursor})
				require.NoError(t, err)
				want := history[offset:min(offset+tt.limit, len(history))]
				require.Equal(t, len(want), len(out.Items))
				for i, item := range out.Items {
					require.Equal(t, want[i].Number, item.Number)
				}
				if offset+tt.limit < len(history) {
					require.NotEmpty(t, out.NextCursor)
				} else {
					require.Empty(t, out.NextCursor)
				}
				cursor = out.NextCursor
			}
		})
	}
}

func TestExecuteRunListChecksRangeBoundary(t *testing.T) {
	for _, tt := range []struct {
		name   string
		change func([]runSummary) []runSummary
		stuck  bool
		want   string
	}{
		{name: "new build shifts range", change: func(history []runSummary) []runSummary {
			return append([]runSummary{{Number: 301, Result: "SUCCESS"}}, history...)
		}},
		{name: "deleted build shifts range", change: func(history []runSummary) []runSummary {
			return history[1:]
		}, want: "build history changed"},
		{name: "server repeats first range", stuck: true, want: "did not advance"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			history := make([]runSummary, 300)
			for i := range history {
				history[i] = runSummary{Number: int64(300 - i), Result: "SUCCESS"}
			}
			rangePattern := regexp.MustCompile(`\{(\d+),(\d+)\}$`)
			client, _ := watchTestClient(t, func(w http.ResponseWriter, r *http.Request) {
				query := rangePattern.FindStringSubmatch(r.URL.Query().Get("tree"))
				if len(query) != 3 {
					t.Errorf("missing tree range: %s", r.URL.RawQuery)
					w.WriteHeader(http.StatusBadRequest)
					return
				}
				start, _ := strconv.Atoi(query[1])
				end, _ := strconv.Atoi(query[2])
				rows := history
				if start > 0 && tt.change != nil {
					rows = tt.change(rows)
				}
				if tt.stuck {
					start, end = 0, end-start
				}
				if err := json.NewEncoder(w).Encode(runListResponse{Builds: rows[min(start, len(rows)):min(end, len(rows))]}); err != nil {
					t.Error(err)
				}
			})
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			out, err := executeRunList(ctx, client, "app", runListOptions{Limit: 100, Cursor: encodeRunCursor("app", 240)})
			if tt.want != "" {
				require.ErrorContains(t, err, tt.want)
				return
			}
			require.NoError(t, err)
			require.Len(t, out.Items, 100)
			for i, item := range out.Items {
				require.Equal(t, int64(239-i), item.Number)
			}
			require.NotEmpty(t, out.NextCursor)
		})
	}
}
