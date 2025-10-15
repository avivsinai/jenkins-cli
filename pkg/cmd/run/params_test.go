package run

import "testing"

func TestParseParametersFromConfig(t *testing.T) {
	xml := `
<project>
  <properties>
    <hudson.model.ParametersDefinitionProperty>
      <parameterDefinitions>
        <hudson.model.StringParameterDefinition>
          <name>ENVIRONMENT</name>
          <defaultValue>prod</defaultValue>
        </hudson.model.StringParameterDefinition>
        <hudson.model.PasswordParameterDefinition>
          <name>SECRET_KEY</name>
          <defaultValue>should-not-appear</defaultValue>
        </hudson.model.PasswordParameterDefinition>
        <hudson.model.ChoiceParameterDefinition>
          <name>REGION</name>
          <choices>
            <string>us-east-1</string>
            <string>eu-west-1</string>
          </choices>
        </hudson.model.ChoiceParameterDefinition>
      </parameterDefinitions>
    </hudson.model.ParametersDefinitionProperty>
  </properties>
</project>`

	params, err := parseParametersFromConfig([]byte(xml))
	if err != nil {
		t.Fatalf("parseParametersFromConfig error: %v", err)
	}
	if len(params) != 3 {
		t.Fatalf("expected 3 parameters, got %d", len(params))
	}

	lookup := make(map[string]runParameterInfo, len(params))
	for _, p := range params {
		lookup[p.Name] = p
	}

	env := lookup["ENVIRONMENT"]
	if env.Default != "prod" {
		t.Fatalf("expected default 'prod', got %q", env.Default)
	}
	if env.Type != "string" {
		t.Fatalf("expected type string, got %s", env.Type)
	}
	if env.IsSecret {
		t.Fatal("ENVIRONMENT should not be marked secret")
	}

	secret := lookup["SECRET_KEY"]
	if !secret.IsSecret {
		t.Fatal("SECRET_KEY should be marked secret")
	}
	if secret.Default != "" {
		t.Fatalf("expected secret default to be redacted, got %q", secret.Default)
	}

	region := lookup["REGION"]
	if region.Type != "choice" {
		t.Fatalf("expected choice type, got %s", region.Type)
	}
	if len(region.SampleValues) != 2 {
		t.Fatalf("expected 2 sample values, got %d", len(region.SampleValues))
	}
}

func TestParameterTypeFromElement(t *testing.T) {
	tests := map[string]struct {
		expectedType   string
		expectedSecret bool
	}{
		"hudson.model.StringParameterDefinition":   {"string", false},
		"hudson.model.PasswordParameterDefinition": {"password", true},
		"My.Custom.SecretParam":                    {"secretparam", true},
	}

	for input, want := range tests {
		gotType, gotSecret := parameterTypeFromElement(input)
		if gotType != want.expectedType || gotSecret != want.expectedSecret {
			t.Fatalf("parameterTypeFromElement(%q) = (%s,%t), expected (%s,%t)", input, gotType, gotSecret, want.expectedType, want.expectedSecret)
		}
	}
}

func TestAppendSampleValue(t *testing.T) {
	values := []string{"a"}
	values = appendSampleValue(values, "b", 5)
	values = appendSampleValue(values, "a", 5)
	values = appendSampleValue(values, "c", 2)
	if len(values) != 2 {
		t.Fatalf("expected capacity limit, got %d values", len(values))
	}
}
