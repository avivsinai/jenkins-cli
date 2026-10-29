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

	"github.com/avivsinai/jenkins-cli/internal/jenkins"
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

		req := client.NewStreamingRequest().
			SetHeader("Accept", "text/plain").
			SetQueryParam("start", strconv.Itoa(offset)).
			SetDoNotParseResponse(true)

		if ctx != nil {
			req.SetContext(ctx)
		}

		resp, err := client.DoRaw(req, http.MethodGet, path, nil)
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
		if resp.StatusCode() >= 400 {
			return fmt.Errorf("stream log for %s #%d: %s", jobPath, buildNumber, resp.Status())
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

// CollectLogSnapshot copies at most maxBytes, even if Jenkins returns the entire
// completed console in one progressive response. The bool indicates omitted data.
func CollectLogSnapshot(ctx context.Context, client *jenkins.Client, jobPath string, buildNumber int, maxBytes int, out io.Writer) (bool, error) {
	encoded := jenkins.EncodeJobPath(jobPath)
	if encoded == "" {
		return false, errors.New("job path is required")
	}
	if maxBytes <= 0 {
		maxBytes = 512 * 1024
	}
	if ctx == nil {
		ctx = context.Background()
	}
	offset := 0
	path := fmt.Sprintf("/%s/%d/logText/progressiveText", encoded, buildNumber)
	total := 0
	for i := 0; i < 1000; i++ {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		req := client.NewStreamingRequest().
			SetHeader("Accept", "text/plain").
			SetQueryParam("start", strconv.Itoa(offset)).
			SetDoNotParseResponse(true).SetContext(ctx)
		resp, err := client.DoRaw(req, http.MethodGet, path, nil)
		if err != nil {
			return false, err
		}
		body := resp.RawBody()
		if body == nil {
			return false, errors.New("log stream returned empty body")
		}
		if resp.StatusCode() == http.StatusRequestedRangeNotSatisfiable {
			_ = body.Close()
			// Restarting after already emitting bytes would duplicate output.
			if total > 0 {
				return true, nil
			}
			offset = 0
			timer := time.NewTimer(150 * time.Millisecond)
			select {
			case <-ctx.Done():
				timer.Stop()
				return false, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if resp.StatusCode() >= 400 {
			_ = body.Close()
			return false, fmt.Errorf("collect log for %s #%d: %s", jobPath, buildNumber, resp.Status())
		}
		remaining := maxBytes - total
		chunk, err := io.ReadAll(io.LimitReader(body, int64(remaining)+1))
		closeErr := body.Close()
		if err != nil || closeErr != nil {
			return false, fmt.Errorf("read log chunk: %w", errors.Join(err, closeErr))
		}
		overflow := len(chunk) > remaining
		if overflow {
			chunk = chunk[:remaining]
		}
		if len(chunk) > 0 {
			if _, err := out.Write(chunk); err != nil {
				return false, err
			}
			total += len(chunk)
		}
		more := strings.EqualFold(resp.Header().Get("X-More-Data"), "true")
		if overflow {
			return true, nil
		}
		if !more {
			return false, nil
		}
		if total >= maxBytes || len(chunk) == 0 {
			return true, nil
		}
		next, err := strconv.Atoi(resp.Header().Get("X-Text-Size"))
		if err != nil || next <= offset {
			return true, nil
		}
		offset = next
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
