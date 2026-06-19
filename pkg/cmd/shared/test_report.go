package shared

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/avivsinai/jenkins-cli/internal/jenkins"
)

type TestCase struct {
	ClassName string  `json:"className"`
	Name      string  `json:"name"`
	Status    string  `json:"status"`
	Duration  float64 `json:"duration"`
}

type TestSuite struct {
	Name  string     `json:"name"`
	Cases []TestCase `json:"cases"`
}

type TestReport struct {
	TotalCount int         `json:"totalCount"`
	FailCount  int         `json:"failCount"`
	SkipCount  int         `json:"skipCount"`
	Suites     []TestSuite `json:"suites"`
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
