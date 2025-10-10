package factory

import (
	"github.com/your-org/jenkins-cli/internal/config"
	"github.com/your-org/jenkins-cli/pkg/cmdutil"
	"github.com/your-org/jenkins-cli/pkg/iostreams"
)

// New constructs a command factory aligned with the GitHub CLI layout but tuned
// for Jenkins. The caller supplies the CLI version string for telemetry/help.
func New(appVersion string) (*cmdutil.Factory, error) {
	ios := iostreams.System()

	f := &cmdutil.Factory{
		AppVersion:     appVersion,
		ExecutableName: "jk",
		IOStreams:      ios,
	}

	f.Config = func() (*config.Config, error) {
		return config.Load()
	}

	return f, nil
}
