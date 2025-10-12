package shared

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/your-org/jenkins-cli/internal/jenkins"
)

func StreamProgressiveLog(ctx context.Context, client *jenkins.Client, jobPath string, buildNumber int, interval time.Duration, out io.Writer) error {
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

		chunk, err := readAndClose(body)
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

func CollectLogSnapshot(ctx context.Context, client *jenkins.Client, jobPath string, buildNumber int, maxBytes int, out io.Writer) (bool, error) {
	encoded := jenkins.EncodeJobPath(jobPath)
	if encoded == "" {
		return false, errors.New("job path is required")
	}

	if maxBytes <= 0 {
		maxBytes = 512 * 1024
	}

	offset := 0
	path := fmt.Sprintf("/%s/%d/logText/progressiveText", encoded, buildNumber)
	total := 0
	truncated := false

	for i := 0; i < 1000; i++ {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return truncated, ctx.Err()
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
				return truncated, ctx.Err()
			}
			return truncated, err
		}

		if resp.StatusCode() == http.StatusRequestedRangeNotSatisfiable {
			offset = 0
			continue
		}

		body := resp.RawBody()
		if body == nil {
			return truncated, errors.New("log stream returned empty body")
		}

		chunk, err := readAndClose(body)
		if err != nil {
			return truncated, fmt.Errorf("read log chunk: %w", err)
		}

		if len(chunk) > 0 {
			if _, err := out.Write(chunk); err != nil {
				return truncated, err
			}
			total += len(chunk)
		}

		if nextOffset := resp.Header().Get("X-Text-Size"); nextOffset != "" {
			if val, err := strconv.Atoi(nextOffset); err == nil {
				offset = val
			}
		}

		more := strings.EqualFold(resp.Header().Get("X-More-Data"), "true")

		switch {
		case !more:
			return truncated, nil
		case len(chunk) == 0:
			return true, nil
		case total >= maxBytes:
			return true, nil
		}
	}

	return true, nil
}

func readAndClose(rc io.ReadCloser) ([]byte, error) {
	data, err := io.ReadAll(rc)
	if cerr := rc.Close(); cerr != nil {
		closeErr := fmt.Errorf("close log stream: %w", cerr)
		if err != nil {
			err = errors.Join(err, closeErr)
		} else {
			err = closeErr
		}
	}
	return data, err
}
