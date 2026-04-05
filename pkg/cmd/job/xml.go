package job

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"regexp"
	"strings"
)

const workflowBranchProjectFactoryClass = "org.jenkinsci.plugins.workflow.multibranch.WorkflowBranchProjectFactory"

var workflowBranchProjectFactoryPattern = regexp.MustCompile(
	fmt.Sprintf(`(?s)<factory\b[^>]*\bclass=(?:"%s"|'%s')[^>]*>.*?</factory>`,
		regexp.QuoteMeta(workflowBranchProjectFactoryClass),
		regexp.QuoteMeta(workflowBranchProjectFactoryClass),
	),
)

func replaceOrInsertElement(configXML, name, replacement string) (string, error) {
	result, err := replaceElement(configXML, name, replacement)
	if err == nil {
		return result, nil
	}
	// Element not found — insert after the opening root tag.
	// Skip any XML declaration (<?xml ... ?>) before looking for the root element.
	rootStart := regexp.MustCompile(`<[a-zA-Z]`).FindStringIndex(configXML)
	if rootStart == nil {
		return "", fmt.Errorf("config.xml has no root element to insert <%s> into", name)
	}
	idx := strings.Index(configXML[rootStart[0]:], ">")
	if idx < 0 {
		return "", fmt.Errorf("config.xml has no root element to insert <%s> into", name)
	}
	insertAt := rootStart[0] + idx + 1
	return configXML[:insertAt] + "\n  " + replacement + configXML[insertAt:], nil
}

func replaceElement(configXML, name, replacement string) (string, error) {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(fmt.Sprintf(`(?s)<%s\b[^>]*/>`, regexp.QuoteMeta(name))),
		regexp.MustCompile(fmt.Sprintf(`(?s)<%s\b[^>]*>.*?</%s>`, regexp.QuoteMeta(name), regexp.QuoteMeta(name))),
	}

	for _, pattern := range patterns {
		if pattern.MatchString(configXML) {
			return pattern.ReplaceAllLiteralString(configXML, replacement), nil
		}
	}

	return "", fmt.Errorf("config.xml does not contain <%s>", name)
}

func replaceOrInsertFactoryChild(configXML, name, replacement string) (string, error) {
	loc := workflowBranchProjectFactoryPattern.FindStringIndex(configXML)
	if loc == nil {
		return "", fmt.Errorf("config.xml does not contain <factory class=\"%s\">", workflowBranchProjectFactoryClass)
	}

	factoryXML := configXML[loc[0]:loc[1]]
	updatedFactoryXML, err := replaceOrInsertElement(factoryXML, name, replacement)
	if err != nil {
		return "", err
	}
	return configXML[:loc[0]] + updatedFactoryXML + configXML[loc[1]:], nil
}

func updateScriptPathInConfig(configXML, scriptPath string) (string, error) {
	if strings.TrimSpace(scriptPath) == "" {
		return "", fmt.Errorf("script path is required")
	}
	return replaceOrInsertFactoryChild(configXML, "scriptPath", fmt.Sprintf("<scriptPath>%s</scriptPath>", xmlEscape(scriptPath)))
}

func xmlEscape(value string) string {
	var buf bytes.Buffer
	if err := xml.EscapeText(&buf, []byte(value)); err != nil {
		return value
	}
	return buf.String()
}
