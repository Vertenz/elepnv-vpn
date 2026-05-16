//go:build !linux

package supervisor

import (
	"context"
	"errors"
	"time"
)

// On non-Linux, xrayd's supervisor is unsupported. The package still compiles
// so editor tooling on macOS dev hosts works; runtime use returns an error.

type Supervisor struct{}

type Child struct {
	Pid  int
	Pgid int
}

type Exit struct {
	Err    error
	Stderr string
}

func (c *Child) ExitC() <-chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

func (c *Child) Result() (Exit, bool) { return Exit{}, false }

func (*Supervisor) Start(_ context.Context, _ string) (*Child, error) {
	return nil, errors.New("supervisor: unsupported on non-linux")
}

func (*Supervisor) Stop(_ context.Context, _ *Child, _ time.Duration) error {
	return errors.New("supervisor: unsupported on non-linux")
}
