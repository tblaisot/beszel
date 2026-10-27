package systems

import (
	"context"
	"time"

	"github.com/henrygd/beszel/internal/common"
)

// controlTimeout bounds a start, stop or restart. Stopping waits for the container's grace period.
const controlTimeout = 65 * time.Second

// ControlOnAgent asks the agent to start, stop or restart a container or a systemd service.
func (sys *System) ControlOnAgent(req common.ControlRequest) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), controlTimeout)
	defer cancel()
	var result string
	err := sys.request(ctx, common.Control, req, &result)
	return result, err
}
