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
