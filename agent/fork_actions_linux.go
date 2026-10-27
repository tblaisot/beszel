//go:build linux

package agent

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/coreos/go-systemd/v22/dbus"
)

var serviceUnitPattern = regexp.MustCompile(`^[a-zA-Z0-9:_.@\\-]+\.service$`)

// otherUnitTypes are the suffixes of units that are not services; they are never controlled.
var otherUnitTypes = []string{".timer", ".socket", ".target", ".mount", ".automount", ".path", ".slice", ".scope", ".swap", ".device"}

// withServiceSuffix completes a service name the way the hub reports it, without the ".service" suffix.
func withServiceSuffix(name string) string {
	if strings.HasSuffix(name, ".service") {
		return name
	}
	return name + ".service"
}

// agentUnitPrefix protects the agent's own unit: stopping it would cut the connection that asked.
const agentUnitPrefix = "beszel-agent"

// serviceAllowed reports whether a unit may be controlled: a plain .service unit that matches
// SERVICE_PATTERNS (the same list the agent already reports), and never the agent itself.
// With the default patterns (*.service) that is every service, so narrow SERVICE_PATTERNS to limit it.
func serviceAllowed(name string, patterns []string) error {
	for _, suffix := range otherUnitTypes {
		if strings.HasSuffix(name, suffix) {
			return errors.New("invalid service name")
		}
	}
	name = withServiceSuffix(name)
	if !serviceUnitPattern.MatchString(name) {
		return errors.New("invalid service name")
	}
	if strings.HasPrefix(name, agentUnitPrefix) {
		return errors.New("the agent cannot control its own service")
	}
	for _, pattern := range patterns {
		if ok, _ := path.Match(pattern, name); ok {
			return nil
		}
	}
	return errors.New("service is not in SERVICE_PATTERNS")
}

// controlService starts, stops or restarts a systemd unit over D-Bus and waits for the job to finish.
func (sm *systemdManager) controlService(ctx context.Context, name, action string) (string, error) {
	if err := serviceAllowed(name, sm.patterns); err != nil {
		return "", err
	}
	name = withServiceSuffix(name)
	conn, err := dbus.NewSystemConnectionContext(ctx)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	jobs := make(chan string, 1)
	switch action {
	case "start":
		_, err = conn.StartUnitContext(ctx, name, "replace", jobs)
	case "stop":
		_, err = conn.StopUnitContext(ctx, name, "replace", jobs)
	case "restart":
		_, err = conn.RestartUnitContext(ctx, name, "replace", jobs)
	default:
		return "", fmt.Errorf("unknown action %q", action)
	}
	if err != nil {
		return "", err
	}
	select {
	case result := <-jobs:
		if result != "done" {
			return "", fmt.Errorf("%s %s: job %s", action, name, result)
		}
		return "ok", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}
