package cmd

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/spf13/cobra"

	"github.com/your-org/jenkins-cli/internal/jenkins"
)

type artifactListResponse struct {
	Artifacts []artifactItem `json:"artifacts"`
}

type artifactItem struct {
	FileName     string `json:"fileName"`
	RelativePath string `json:"relativePath"`
	Size         int64  `json:"size"`
}

func newArtifactCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "artifact",
		Short: "Work with run artifacts",
	}

	cmd.AddCommand(
		newArtifactListCmd(),
		newArtifactDownloadCmd(),
	)

	return cmd
}

func newArtifactListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ls <jobPath> <buildNumber>",
		Short: "List artifacts for a run",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			items, err := fetchArtifacts(cmd, args[0], args[1])
			if err != nil {
				return err
			}

			return printOutput(cmd, items, func() error {
				if len(items) == 0 {
					fmt.Fprintln(cmd.OutOrStdout(), "No artifacts found")
					return nil
				}
				for _, item := range items {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%d bytes\n", item.RelativePath, item.FileName, item.Size)
				}
				return nil
			})
		},
	}

	return cmd
}

func newArtifactDownloadCmd() *cobra.Command {
	var pattern string
	var outputDir string
	var allowEmpty bool

	cmd := &cobra.Command{
		Use:   "download <jobPath> <buildNumber>",
		Short: "Download artifacts",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			items, err := fetchArtifacts(cmd, args[0], args[1])
			if err != nil {
				return err
			}

			if pattern == "" {
				pattern = "**/*"
			}

			matched := make([]artifactItem, 0, len(items))
			for _, item := range items {
				match, err := doublestar.Match(pattern, item.RelativePath)
				if err != nil {
					return err
				}
				if match {
					matched = append(matched, item)
				}
			}

			if len(matched) == 0 {
				if allowEmpty {
					fmt.Fprintln(cmd.OutOrStdout(), "No artifacts matched pattern")
					return nil
				}
				return newExitError(3, "no artifacts matched pattern")
			}

			client, err := newJenkinsClient(cmd)
			if err != nil {
				return err
			}

			num, err := strconv.Atoi(args[1])
			if err != nil {
				return err
			}

			encoded := jenkins.EncodeJobPath(args[0])
			base := fmt.Sprintf("/%s/%d/artifact", encoded, num)

			for _, art := range matched {
				destPath := filepath.Join(outputDir, art.RelativePath)
				if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
					return err
				}

				req := client.NewRequest().SetDoNotParseResponse(true)
				resp, err := client.Do(req, http.MethodGet, fmt.Sprintf("%s/%s", base, art.RelativePath), nil)
				if err != nil {
					return err
				}

				body := resp.RawBody()
				if body == nil {
					return errors.New("artifact response empty")
				}
				file, err := os.Create(destPath)
				if err != nil {
					body.Close()
					return err
				}
				if _, err := io.Copy(file, body); err != nil {
					body.Close()
					file.Close()
					return err
				}
				body.Close()
				file.Close()
				fmt.Fprintf(cmd.OutOrStdout(), "Downloaded %s\n", destPath)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&pattern, "pattern", "p", "**/*", "Glob to match artifacts")
	cmd.Flags().StringVarP(&outputDir, "output", "o", ".", "Output directory")
	cmd.Flags().BoolVar(&allowEmpty, "allow-empty", false, "Do not error when no artifacts match")
	return cmd
}

func fetchArtifacts(cmd *cobra.Command, jobPath, buildNumber string) ([]artifactItem, error) {
	client, err := newJenkinsClient(cmd)
	if err != nil {
		return nil, err
	}

	num, err := strconv.Atoi(buildNumber)
	if err != nil {
		return nil, err
	}

	encoded := jenkins.EncodeJobPath(jobPath)
	if encoded == "" {
		return nil, errors.New("job path is required")
	}
	path := fmt.Sprintf("/%s/%d/api/json", encoded, num)

	var resp artifactListResponse
	_, err = client.Do(client.NewRequest().SetQueryParam("tree", "artifacts[fileName,relativePath,size]"), http.MethodGet, path, &resp)
	if err != nil {
		return nil, err
	}

	return resp.Artifacts, nil
}
