package agent

import (
	"log"

	"github.com/henrygd/beszel/agent/utils"
)

// refuseIfForkUpdatesDisabled stops `beszel-agent update` when FORK_DISABLE_UPDATE=true: this
// fork's binaries are installed by code, and the upstream updater would replace the binary with
// upstream's own release (without the fork's features).
func refuseIfForkUpdatesDisabled() {
	if value, _ := utils.GetEnv("FORK_DISABLE_UPDATE"); value == "true" {
		log.Fatal("updates from the command line are disabled (FORK_DISABLE_UPDATE=true); deploy a new release instead")
	}
}
