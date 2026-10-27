//go:build testing

package hub

import (
	"github.com/henrygd/beszel/internal/common"
	"github.com/henrygd/beszel/internal/hub/systems"
)

// SetForkControlAgent replaces the call to the agent and returns a function that restores it.
// Tests use it to stand in for an agent.
func SetForkControlAgent(fn func(sys *systems.System, req common.ControlRequest) (string, error)) (restore func()) {
	previous := forkControlAgent
	forkControlAgent = fn
	return func() { forkControlAgent = previous }
}

// ExecStream is the agent side of a console, as far as the hub is concerned.
type ExecStream interface {
	Send(common.ExecFrame) error
	Recv() (common.ExecFrame, error)
	Close()
}

// SetForkOpenExec replaces the call that opens a console on the agent and returns a function that
// restores it. Tests use it to stand in for an agent.
func SetForkOpenExec(fn func(sys *systems.System, req common.ExecRequest) (ExecStream, error)) (restore func()) {
	previous := forkOpenExec
	forkOpenExec = func(sys *systems.System, req common.ExecRequest) (execStream, error) {
		stream, err := fn(sys, req)
		if err != nil {
			return nil, err
		}
		return stream, nil
	}
	return func() { forkOpenExec = previous }
}
