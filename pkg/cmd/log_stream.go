package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/your-org/jenkins-cli/pkg/jenkins"
)

func streamProgressiveLog(ctx context.Context, client *jenkins.Client, jobPath string, buildNumber int, interval time.Duration, out io.Writer) error {
	encoded := jenkins.EncodeJobPath(jobPath)
	if encoded == "" {
		return errors.New("job path is required")
	}

	offset := 0
	path := fmt.Sprintf("/%s/%d/logText/progressiveText", encoded, buildNumber)

	for {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
			}
		}

		req := client.NewRequest().
			SetHeader("Accept", "text/plain").
			SetQueryParam("start", strconv.Itoa(offset)).
			SetDoNotParseResponse(true)

		if ctx != nil {
			req.SetContext(ctx)
		}

		resp, err := client.Do(req, http.MethodGet, path, nil)
		if err != nil {
			if ctx != nil && ctx.Err() != nil {
				return nil
			}
			return err
		}

		if resp.StatusCode() == http.StatusRequestedRangeNotSatisfiable {
			offset = 0
			time.Sleep(interval)
			continue
		}

		body := resp.RawBody()
		if body == nil {
			return errors.New("log stream returned empty body")
		}

		chunk, err := io.ReadAll(body)
		body.Close()
		if err != nil {
			if ctx != nil && ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("read log chunk: %w", err)
		}

		if len(chunk) > 0 {
			if _, err := out.Write(chunk); err != nil {
				return err
			}
		}

		if nextOffset := resp.Header().Get("X-Text-Size"); nextOffset != "" {
			if val, err := strconv.Atoi(nextOffset); err == nil {
				offset = val
			}
		}

		if strings.EqualFold(resp.Header().Get("X-More-Data"), "true") {
			time.Sleep(interval)
			continue
		}

		return nil
	}
}
