package e2e

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRunStartExtendedChoiceCheckbox(t *testing.T) {
	h := requireHarness(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	var plugins struct {
		Plugins []struct {
			ShortName string `json:"shortName"`
			Version   string `json:"version"`
			Active    bool   `json:"active"`
		} `json:"plugins"`
	}
	headers := readFixtureJSON(t, ctx, h, "/pluginManager/api/json?tree=plugins[shortName,version,active]", &plugins)
	var pluginVersion string
	for _, plugin := range plugins.Plugins {
		if plugin.ShortName == "extended-choice-parameter" && plugin.Active {
			pluginVersion = plugin.Version
			break
		}
	}
	require.NotEmpty(t, pluginVersion, "the controller fixture must have the real Extended Choice Parameter plugin active")
	t.Logf("Jenkins %s, Extended Choice Parameter %s", headers.Get("X-Jenkins"), pluginVersion)

	const contextName = "extended-choice-e2e"
	_, stderr, err := h.runCLI(ctx, "auth", "login", h.baseURL, "--username", h.adminUser, "--token", h.adminPassword, "--name", contextName)
	require.NoError(t, err, "login: %s", stderr)

	const job = "jk-extended-choice"
	const config = `<project>
  <quietPeriod>0</quietPeriod>
  <properties><hudson.model.ParametersDefinitionProperty><parameterDefinitions>
    <com.cwctravel.hudson.plugins.extended__choice__parameter.ExtendedChoiceParameterDefinition>
      <name>DOMAINS</name><type>PT_CHECKBOX</type><value>a,b,c</value>
      <defaultValue>a</defaultValue><multiSelectDelimiter>,</multiSelectDelimiter>
    </com.cwctravel.hudson.plugins.extended__choice__parameter.ExtendedChoiceParameterDefinition>
    <hudson.model.StringParameterDefinition>
      <name>CONFIG</name><defaultValue>unset</defaultValue><trim>false</trim>
    </hudson.model.StringParameterDefinition>
  </parameterDefinitions></hudson.model.ParametersDefinitionProperty></properties>
  <builders/>
</project>`
	_, stderr, err = h.runCLIWithInput(ctx, config, "--context", contextName, "job", "create", job, "--stdin")
	require.NoError(t, err, "create fixture job: %s", stderr)

	stdout, stderr, err := h.runCLI(ctx, "--context", contextName, "run", "params", job, "--source", "config", "--json")
	require.NoError(t, err, "discover parameters: %s", stderr)
	var discovered struct {
		Parameters []struct {
			Name      string `json:"name"`
			InputFlag string `json:"inputFlag"`
		} `json:"parameters"`
	}
	require.NoError(t, json.Unmarshal([]byte(stdout), &discovered))
	flags := make(map[string]string)
	for _, param := range discovered.Parameters {
		flags[param.Name] = param.InputFlag
	}
	require.Equal(t, map[string]string{"CONFIG": "", "DOMAINS": "--param-list"}, flags)
	t.Logf("jk run params %s --source config --json reports %v", job, flags)

	for _, trial := range []struct {
		args []string
		want map[string]string
	}{
		{[]string{"-p", "DOMAINS=a,b,c", "-p", "CONFIG="}, map[string]string{"DOMAINS": "", "CONFIG": ""}},
		{[]string{"--param-list", `DOMAINS=["a","b","c"]`, "-p", "CONFIG=a=1,b=2"}, map[string]string{"DOMAINS": "a,b,c", "CONFIG": "a=1,b=2"}},
	} {
		args := append([]string{"run", "start", job, "--wait", "--interval", "1s", "--json", "--context", contextName}, trial.args...)
		stdout, stderr, err := h.runCLI(ctx, args...)
		require.NoError(t, err, "start fixture job: %s\n%s", stdout, stderr)
		var build struct {
			Number  int    `json:"number"`
			Result  string `json:"result"`
			Actions []struct {
				Parameters []struct {
					Name  string `json:"name"`
					Value string `json:"value"`
				} `json:"parameters"`
			} `json:"actions"`
		}
		readFixtureJSON(t, ctx, h, "/job/"+job+"/lastBuild/api/json?tree=number,result,actions[parameters[name,value]]", &build)
		recorded := make(map[string]string)
		for _, action := range build.Actions {
			for _, param := range action.Parameters {
				recorded[param.Name] = param.Value
			}
		}
		require.Equal(t, "SUCCESS", build.Result)
		require.Equal(t, trial.want, recorded)
		t.Logf("jk %q -> Jenkins build #%d recorded %q", args, build.Number, recorded)
	}

	for _, flags := range [][]string{
		{"-p", "DOMAINS=a", "-p", "DOMAINS=b", "-p", "DOMAINS=c"},
		{"--param-list", `DOMAINS=["a","b"]`, "-p", "DOMAINS=c"},
		{"--param-list", `DOMAINS=["a"]`, "--param-list", `DOMAINS=["b"]`},
	} {
		args := append([]string{"run", "start", job, "--context", contextName}, flags...)
		_, stderr, err := h.runCLI(ctx, args...)
		require.Error(t, err)
		require.Contains(t, stderr, `duplicate parameter "DOMAINS"`)
		t.Logf("jk %q -> %s", args, stderr)
	}
	var state struct {
		NextBuildNumber int  `json:"nextBuildNumber"`
		InQueue         bool `json:"inQueue"`
	}
	readFixtureJSON(t, ctx, h, "/job/"+job+"/api/json?tree=nextBuildNumber,inQueue", &state)
	require.Equal(t, 3, state.NextBuildNumber)
	require.False(t, state.InQueue)
}

func readFixtureJSON(t *testing.T, ctx context.Context, h *harness, path string, result any) http.Header {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.baseURL+path, nil)
	require.NoError(t, err)
	req.SetBasicAuth(h.adminUser, h.adminPassword)
	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode, "GET %s", path)
	require.NoError(t, json.NewDecoder(resp.Body).Decode(result), "decode %s", path)
	return resp.Header
}
