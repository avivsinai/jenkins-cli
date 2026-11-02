package shared

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStreamAndClose(t *testing.T) {
	readErr := errors.New("read failed")
	closeErr := errors.New("close failed")
	writeErr := errors.New("write failed")
	for _, tc := range []struct {
		name        string
		readErr     error
		closeErr    error
		writeErr    error
		shortWrite  bool
		cancelRead  bool
		cancelWrite bool
		wantErr     error
	}{
		{name: "success"},
		{name: "read error", readErr: readErr, wantErr: readErr},
		{name: "close error", closeErr: closeErr, wantErr: closeErr},
		{name: "read and close errors", readErr: readErr, closeErr: closeErr, wantErr: readErr},
		{name: "write error", writeErr: writeErr, wantErr: writeErr},
		{name: "write and close errors", writeErr: writeErr, closeErr: closeErr, wantErr: writeErr},
		{name: "short write", shortWrite: true, wantErr: io.ErrShortWrite},
		{name: "canceled read", cancelRead: true, readErr: context.Canceled, closeErr: closeErr},
		{name: "canceled write still reports output error", cancelWrite: true, writeErr: writeErr, wantErr: writeErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			reader := io.Reader(strings.NewReader("hello\n"))
			if tc.readErr != nil {
				reader = logReaderFunc(func([]byte) (int, error) {
					if tc.cancelRead {
						cancel()
					}
					return 0, tc.readErr
				})
			}
			body := &logTestBody{Reader: reader, closeErr: tc.closeErr}
			var out bytes.Buffer
			writer := logWriterFunc(func(p []byte) (int, error) {
				if tc.cancelWrite {
					cancel()
				}
				if tc.writeErr != nil {
					return 0, tc.writeErr
				}
				if tc.shortWrite {
					return len(p) - 1, nil
				}
				return out.Write(p)
			})
			err := streamAndClose(ctx, body, writer)
			require.Equal(t, 1, body.closed)
			if tc.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.wantErr)
				if tc.closeErr != nil && ctx.Err() == nil {
					require.ErrorIs(t, err, tc.closeErr)
				}
				if tc.readErr != nil {
					require.ErrorContains(t, err, "read log chunk")
				}
			}
			if tc.name == "success" {
				require.Equal(t, "hello\n", out.String())
			}
		})
	}
}

type logReaderFunc func([]byte) (int, error)

func (f logReaderFunc) Read(p []byte) (int, error) { return f(p) }

type logWriterFunc func([]byte) (int, error)

func (f logWriterFunc) Write(p []byte) (int, error) { return f(p) }

type logTestBody struct {
	io.Reader
	closeErr error
	closed   int
}

func (b *logTestBody) Close() error {
	b.closed++
	return b.closeErr
}
