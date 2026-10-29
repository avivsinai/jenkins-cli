package shared

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/avivsinai/jenkins-cli/internal/jenkins"
)

type TestCase struct {
	ClassName       string  `json:"className"`
	Name            string  `json:"name"`
	Status          string  `json:"status"`
	Duration        float64 `json:"duration"`
	ErrorDetails    string  `json:"errorDetails,omitempty"`
	ErrorStackTrace string  `json:"errorStackTrace,omitempty"`
	Age             int     `json:"age,omitempty"`
	FailedSince     int     `json:"failedSince,omitempty"`
}

// Failed includes both a newly failing test and a repeated failure in Jenkins.
func (c TestCase) Failed() bool {
	switch strings.ToUpper(strings.TrimSpace(c.Status)) {
	case "FAILED", "REGRESSION":
		return true
	default:
		return false
	}
}

type TestSuite struct {
	Name  string     `json:"name"`
	Cases []TestCase `json:"cases"`
}

type TestReport struct {
	TotalCount int         `json:"totalCount"`
	PassCount  int         `json:"passCount,omitempty"`
	FailCount  int         `json:"failCount"`
	SkipCount  int         `json:"skipCount"`
	Suites     []TestSuite `json:"suites"`
}

// UnmarshalJSON accounts for JUnit reports that export pass/fail/skip counts
// without totalCount. An explicitly supplied total, including zero, wins.
func (r *TestReport) UnmarshalJSON(data []byte) error {
	type plain TestReport
	var decoded plain
	aux := struct {
		*plain
		TotalCount *int `json:"totalCount"`
	}{plain: &decoded}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	*r = TestReport(decoded)
	if aux.TotalCount != nil {
		r.TotalCount = *aux.TotalCount
	} else {
		r.TotalCount = r.PassCount + r.FailCount + r.SkipCount
	}
	return nil
}

// FailedOnly filters case details while retaining the complete report's counts.
// It returns a copy so callers can still use the original suites and cases.
func (r *TestReport) FailedOnly() *TestReport {
	filtered := *r
	filtered.Suites = make([]TestSuite, 0)
	for _, suite := range r.Suites {
		kept := TestSuite{Name: suite.Name, Cases: make([]TestCase, 0)}
		for _, c := range suite.Cases {
			if c.Failed() {
				kept.Cases = append(kept.Cases, c)
			}
		}
		if len(kept.Cases) > 0 {
			filtered.Suites = append(filtered.Suites, kept)
		}
	}
	return &filtered
}

func FetchTestReport(client *jenkins.Client, jobPath string, buildNumber int64) (*TestReport, error) {
	if client == nil {
		return nil, errors.New("jenkins client is required")
	}
	if jobPath == "" {
		return nil, errors.New("job path is required")
	}
	if buildNumber <= 0 {
		return nil, errors.New("build number must be positive")
	}

	path := fmt.Sprintf("/%s/%d/testReport/api/json", jenkins.EncodeJobPath(jobPath), buildNumber)
	req := client.NewRequest()

	var report TestReport
	resp, err := client.DoRaw(req, http.MethodGet, path, &report)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode() == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode() >= 400 {
		return nil, fmt.Errorf("fetch test report failed: %s", resp.Status())
	}

	return &report, nil
}
