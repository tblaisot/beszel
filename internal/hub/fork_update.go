package hub

import (
	"log"

	"github.com/henrygd/beszel/internal/hub/utils"
)

// forkUpdatesDisabled reports whether the operator turned off updating from the running binary
// (FORK_DISABLE_UPDATE=true). Releases of this fork are installed by code, and the upstream updater
// would replace the binary with upstream's own release. When set, the "new version available" check
// (and its GitHub request) is off even if CHECK_UPDATES is set, and `beszel update` refuses.
func forkUpdatesDisabled() bool {
	value, _ := utils.GetEnv("FORK_DISABLE_UPDATE")
	return value == "true"
}

func refuseIfForkUpdatesDisabled() {
	if forkUpdatesDisabled() {
		log.Fatal("updates from the command line are disabled (FORK_DISABLE_UPDATE=true); deploy a new release instead")
	}
}
