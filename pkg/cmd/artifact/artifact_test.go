package artifact

import (
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type fakeArtifactResponse struct {
	code   int
	status string
	body   io.ReadCloser
}

func (f *fakeArtifactResponse) StatusCode() int        { return f.code }
func (f *fakeArtifactResponse) Status() string         { return f.status }
func (f *fakeArtifactResponse) RawBody() io.ReadCloser { return f.body }

type trackingCloser struct {
	io.Reader
	closed bool
}

func (t *trackingCloser) Close() error {
	t.closed = true
	return nil
}

func TestSanitizeArtifactPath_DisallowsTraversal(t *testing.T) {
	outputDir := t.TempDir()

	_, _, _, err := sanitizeArtifactPath(outputDir, outputDir, "../escape/outside.txt")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsafe artifact path")
}

func TestSanitizeArtifactPath_AllowsNestedPath(t *testing.T) {
	outputDirAbs := t.TempDir()
	outputDir := "downloads"

	dest, display, clean, err := sanitizeArtifactPath(outputDirAbs, outputDir, "nested/file with space.txt")
	require.NoError(t, err)
	require.Equal(t, "nested/file with space.txt", clean)
	require.Equal(t, filepath.Join(outputDirAbs, "nested", "file with space.txt"), dest)
	require.Equal(t, filepath.Join(outputDir, filepath.FromSlash(clean)), display)
}

func TestEnsureArtifactResponse_ErrorsOnNonSuccess(t *testing.T) {
	rc := &trackingCloser{Reader: strings.NewReader("failure")}
	resp := &fakeArtifactResponse{
		code:   404,
		status: "404 Not Found",
		body:   rc,
	}

	body, err := ensureArtifactResponse("bad.txt", resp)
	require.Error(t, err)
	require.Nil(t, body)
	require.True(t, rc.closed, "expected response body to be closed")
}

func TestEnsureArtifactResponse_ReturnsBodyOnSuccess(t *testing.T) {
	rc := &trackingCloser{Reader: strings.NewReader("data")}
	resp := &fakeArtifactResponse{
		code:   200,
		status: "200 OK",
		body:   rc,
	}

	body, err := ensureArtifactResponse("good.txt", resp)
	require.NoError(t, err)
	require.Equal(t, rc, body)
	require.False(t, rc.closed, "body should not be closed on success")
}

func TestEnsureArtifactResponse_EmptyBody(t *testing.T) {
	resp := &fakeArtifactResponse{
		code:   200,
		status: "200 OK",
		body:   nil,
	}

	body, err := ensureArtifactResponse("empty.txt", resp)
	require.Error(t, err)
	require.ErrorContains(t, err, "artifact response empty")
	require.Nil(t, body)
}
