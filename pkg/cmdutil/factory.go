package cmdutil

import (
	"context"
	"sync"

	"github.com/your-org/jenkins-cli/internal/config"
	"github.com/your-org/jenkins-cli/internal/jenkins"
	"github.com/your-org/jenkins-cli/pkg/iostreams"
)

// Factory wires together shared services used by Cobra commands. It mirrors the
// GitHub CLI structure but swaps in Jenkins aware components.
type Factory struct {
	AppVersion     string
	ExecutableName string

	IOStreams *iostreams.IOStreams

	Config        func() (*config.Config, error)
	JenkinsClient func(context.Context, string) (*jenkins.Client, error)

	once struct {
		cfg sync.Once
	}
	cfg    *config.Config
	cfgErr error
	ioOnce sync.Once
	ios    *iostreams.IOStreams
}

// ResolveConfig eagerly loads the CLI configuration, caching the result.
func (f *Factory) ResolveConfig() (*config.Config, error) {
	f.once.cfg.Do(func() {
		if f.Config == nil {
			f.cfg, f.cfgErr = config.Load()
			return
		}
		f.cfg, f.cfgErr = f.Config()
	})
	return f.cfg, f.cfgErr
}

// Streams returns the IO streams, initialising them lazily.
func (f *Factory) Streams() (*iostreams.IOStreams, error) {
	f.ioOnce.Do(func() {
		if f.IOStreams != nil {
			f.ios = f.IOStreams
			return
		}
		f.ios = iostreams.System()
	})
	return f.ios, nil
}

// Client returns a Jenkins client for the requested context.
func (f *Factory) Client(ctx context.Context, contextName string) (*jenkins.Client, error) {
	cfg, err := f.ResolveConfig()
	if err != nil {
		return nil, err
	}

	if f.JenkinsClient != nil {
		return f.JenkinsClient(ctx, contextName)
	}
	return jenkins.NewClient(ctx, cfg, contextName)
}
