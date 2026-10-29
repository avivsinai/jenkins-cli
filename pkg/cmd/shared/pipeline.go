package shared

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/avivsinai/jenkins-cli/internal/jenkins"
)

// PipelineNode is the reader-accessible Pipeline Graph View node identity.
// IDs, rather than labels, distinguish parallel branches and repeated steps.
type PipelineNode struct {
	ID          string         `json:"id" yaml:"id"`
	Name        string         `json:"name" yaml:"name"`
	State       string         `json:"state" yaml:"state"`
	Type        string         `json:"type" yaml:"type"`
	Title       string         `json:"title,omitempty" yaml:"title,omitempty"`
	StageID     string         `json:"stageId,omitempty" yaml:"stageId,omitempty"`
	Children    []PipelineNode `json:"children,omitempty" yaml:"children,omitempty"`
	NextSibling *PipelineNode  `json:"nextSibling,omitempty" yaml:"nextSibling,omitempty"`
}

type PipelineSteps struct {
	JobPath  string         `json:"jobPath" yaml:"jobPath"`
	Build    int64          `json:"build" yaml:"build"`
	Complete bool           `json:"complete" yaml:"complete"`
	Stages   []PipelineNode `json:"stages" yaml:"stages"`
	Steps    []PipelineNode `json:"steps" yaml:"steps"`
}

// FetchPipelineSteps checks that the run exists before probing optional plugin
// routes. An absent route on an existing run is an unsupported capability;
// authentication failures are retained as HTTP errors, not capability failures.
func FetchPipelineSteps(ctx context.Context, client *jenkins.Client, jobPath string, build int64) (*PipelineSteps, error) {
	if client == nil {
		return nil, errors.New("jenkins client is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	encoded := jenkins.EncodeJobPath(jobPath)
	if encoded == "" || build <= 0 {
		return nil, errors.New("job path and positive build number are required")
	}
	base := fmt.Sprintf("/%s/%d", encoded, build)
	req := client.NewRequest().SetContext(ctx).SetQueryParam("tree", "number")
	resp, err := client.DoRaw(req, http.MethodGet, base+"/api/json", nil)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode() == http.StatusNotFound {
		return nil, NewExitError(3, fmt.Sprintf("run %s #%d not found", jobPath, build))
	}
	if resp.StatusCode() >= 400 {
		return nil, fmt.Errorf("view run %s #%d: %s", jobPath, build, resp.Status())
	}

	var graph struct {
		Stages   []PipelineNode `json:"stages" yaml:"stages"`
		Complete bool           `json:"complete" yaml:"complete"`
	}
	if err := fetchPipelineData(ctx, client, base+"/stages/tree", "stages", &graph); err != nil {
		return nil, err
	}
	var list struct {
		Steps         []PipelineNode `json:"steps" yaml:"steps"`
		RunIsComplete bool           `json:"runIsComplete" yaml:"runIsComplete"`
	}
	if err := fetchPipelineData(ctx, client, base+"/stages/allSteps", "steps", &list); err != nil {
		return nil, err
	}
	if graph.Stages == nil {
		graph.Stages = []PipelineNode{}
	}
	if list.Steps == nil {
		list.Steps = []PipelineNode{}
	}
	return &PipelineSteps{JobPath: jobPath, Build: build, Complete: graph.Complete && list.RunIsComplete, Stages: graph.Stages, Steps: list.Steps}, nil
}

func fetchPipelineData(ctx context.Context, client *jenkins.Client, path, required string, dst any) error {
	// Metadata is normally small. Bound malformed/unexpected plugin responses.
	const maxMetadata = 16 * 1024 * 1024
	req := client.NewRequest().SetContext(ctx).SetDoNotParseResponse(true)
	resp, err := client.DoRaw(req, http.MethodGet, path, nil)
	if err != nil {
		return err
	}
	body := resp.RawBody()
	if body == nil {
		return errors.New("pipeline graph view returned an empty response")
	}
	defer func() { _ = body.Close() }()
	if resp.StatusCode() == http.StatusNotFound {
		return NewExitError(8, "pipeline graph view is unavailable for this run (install/enable the plugin on a Pipeline job)")
	}
	if resp.StatusCode() >= 400 {
		return fmt.Errorf("pipeline graph view request: %s", resp.Status())
	}
	data, err := io.ReadAll(io.LimitReader(body, maxMetadata+1))
	if err != nil {
		return fmt.Errorf("read Pipeline Graph View response: %w", err)
	}
	if len(data) > maxMetadata {
		return errors.New("pipeline graph view metadata exceeds 16 MiB")
	}
	var envelope struct {
		Status string          `json:"status" yaml:"status"`
		Data   json.RawMessage `json:"data" yaml:"data"`
	}
	if err := json.Unmarshal(data, &envelope); err != nil {
		return fmt.Errorf("decode Pipeline Graph View response: %w", err)
	}
	if envelope.Status != "ok" {
		return fmt.Errorf("pipeline graph view response status %q (expected ok)", envelope.Status)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(envelope.Data, &fields); err != nil {
		return fmt.Errorf("decode Pipeline Graph View data: %w", err)
	}
	if _, ok := fields[required]; !ok {
		return fmt.Errorf("pipeline graph view response is missing %q", required)
	}
	if err := json.Unmarshal(envelope.Data, dst); err != nil {
		return fmt.Errorf("decode Pipeline Graph View data: %w", err)
	}
	return nil
}

// HasNode accepts a stage or step ID returned by the plugin. The plugin's log
// endpoint otherwise returns HTTP 200 even for an invalid node ID.
func (p *PipelineSteps) HasNode(id string) bool {
	for _, step := range p.Steps {
		if step.ID == id {
			return true
		}
	}
	var contains func([]PipelineNode) bool
	contains = func(nodes []PipelineNode) bool {
		for _, node := range nodes {
			if node.ID == id || contains(node.Children) {
				return true
			}
			if node.NextSibling != nil && contains([]PipelineNode{*node.NextSibling}) {
				return true
			}
		}
		return false
	}
	return contains(p.Stages)
}
