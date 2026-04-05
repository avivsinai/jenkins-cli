package job

import (
	"errors"
	"fmt"
	"strings"
)

const maxJenkinsErrorDetail = 200

func responseStatusError(action, status, body string) error {
	message := fmt.Sprintf("%s failed: %s", action, status)
	if detail := compactResponseBody(body); detail != "" {
		message += ": " + detail
	}
	return errors.New(message)
}

func compactResponseBody(body string) string {
	detail := strings.TrimSpace(body)
	if detail == "" {
		return ""
	}

	detail = strings.Join(strings.Fields(detail), " ")
	if len(detail) > maxJenkinsErrorDetail {
		return detail[:maxJenkinsErrorDetail] + "..."
	}
	return detail
}
