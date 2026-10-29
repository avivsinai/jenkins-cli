package logcmd

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"

	"github.com/spf13/cobra"

	"github.com/avivsinai/jenkins-cli/internal/jenkins"
)

// A finite line limit makes regular-expression matching and the tail accumulator
// bounded even if a controller returns one enormous line. Fail instead of
// silently dropping a possible match or returning an inexact tail.
const maxFilteredLineBytes = 1024 * 1024

func validateLogFilters(cmd *cobra.Command, opts *logOptions) error {
	tailSet := cmd.Flags().Changed("tail")
	grepSet := cmd.Flags().Changed("grep")
	if tailSet && opts.tailLines <= 0 {
		return errors.New("--tail must be positive")
	}
	if opts.follow && (tailSet || grepSet) {
		return errors.New("--tail/--grep cannot be combined with --follow")
	}
	if grepSet {
		pattern, err := regexp.Compile(opts.grepPattern)
		if err != nil {
			return fmt.Errorf("invalid --grep regular expression: %w", err)
		}
		opts.grep = pattern
	}
	return nil
}

func hasLogFilters(opts *logOptions) bool {
	return opts.tailLines > 0 || opts.grep != nil
}

func collectFilteredLog(ctx context.Context, client *jenkins.Client, opts *logOptions, buildNumber int) ([]byte, bool, error) {
	// consoleText streams the complete, already-finished console. Do not apply
	// the legacy progressive snapshot limit before selecting its tail.
	path := fmt.Sprintf("/%s/%d/consoleText", jenkins.EncodeJobPath(opts.jobPath), buildNumber)
	req := client.NewStreamingRequest().SetContext(ctx).
		SetHeader("Accept", "text/plain").SetDoNotParseResponse(true)
	resp, err := client.DoRaw(req, http.MethodGet, path, nil)
	if err != nil {
		return nil, false, err
	}
	body := resp.RawBody()
	if body == nil {
		return nil, false, errors.New("console log returned an empty response body")
	}
	defer func() { _ = body.Close() }()
	if resp.StatusCode() >= 400 {
		return nil, false, fmt.Errorf("collect log for %s #%d: %s", opts.jobPath, buildNumber, resp.Status())
	}
	return selectLogLines(ctx, body, opts)
}

// consoleLines retains line endings, including CRLF and an absent final newline.
// Match semantics exclude the line ending, as line-oriented grep tools do.
func consoleLines(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i+1], nil
	}
	if atEOF && len(data) > 0 {
		return len(data), data, nil
	}
	return 0, nil, nil
}

func selectLogLines(ctx context.Context, input io.Reader, opts *logOptions) ([]byte, bool, error) {
	maxBytes := opts.maxBytes
	if maxBytes <= 0 {
		maxBytes = 2 * 1024 * 1024
	}
	selected := newLineRing(maxBytes)
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 64*1024), maxFilteredLineBytes+1)
	scanner.Split(consoleLines)
	matched := uint64(0)
	omitted := false
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		line := scanner.Bytes()
		if len(line) > maxFilteredLineBytes {
			return nil, false, fmt.Errorf("console line exceeds %d bytes; cannot select an exact log tail", maxFilteredLineBytes)
		}
		text := line
		if bytes.HasSuffix(text, []byte{'\n'}) {
			text = bytes.TrimSuffix(text, []byte{'\n'})
			text = bytes.TrimSuffix(text, []byte{'\r'})
		}
		if opts.grep != nil && !opts.grep.Match(text) {
			continue
		}
		matched++
		if opts.tailLines > 0 {
			for selected.lines >= opts.tailLines || selected.size+len(line) > maxBytes {
				if selected.lines == 0 {
					return nil, false, fmt.Errorf("console line exceeds the %d-byte output budget", maxBytes)
				}
				selected.dropLine()
			}
			selected.append(line)
		} else if !omitted && selected.size+len(line) <= maxBytes {
			selected.append(line)
		} else {
			// Keep a prefix for grep-only output, while scanning every line so
			// completeness is never inferred from an early bounded snapshot.
			omitted = true
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, false, fmt.Errorf("scan console log (maximum line %d bytes): %w", maxFilteredLineBytes, err)
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if opts.tailLines > 0 {
		wanted := matched
		if wanted > uint64(opts.tailLines) {
			wanted = uint64(opts.tailLines)
		}
		omitted = uint64(selected.lines) < wanted
	}
	return selected.bytes(), omitted, nil
}

// lineRing stores at most the output byte budget regardless of the requested
// number of lines. It neither allocates N slots nor retains evicted strings.
type lineRing struct {
	data  []byte
	start int
	size  int
	lines int
}

func newLineRing(capacity int) *lineRing {
	return &lineRing{data: make([]byte, capacity)}
}

func (r *lineRing) dropLine() {
	for r.size > 0 {
		b := r.data[r.start]
		r.start = (r.start + 1) % len(r.data)
		r.size--
		if b == '\n' {
			break
		}
	}
	r.lines--
}

func (r *lineRing) append(line []byte) {
	end := (r.start + r.size) % len(r.data)
	n := copy(r.data[end:], line)
	copy(r.data, line[n:])
	r.size += len(line)
	r.lines++
}

func (r *lineRing) bytes() []byte {
	out := make([]byte, r.size)
	n := copy(out, r.data[r.start:])
	copy(out[n:], r.data[:r.size-n])
	return out
}
