//go:build !linux

package agent

import (
	"context"
	"errors"
)

// systemd is Linux-only.
func (sm *systemdManager) controlService(_ context.Context, _, _ string) (string, error) {
	return "", errors.ErrUnsupported
}
