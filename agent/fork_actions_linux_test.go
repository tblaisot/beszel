//go:build linux && testing

package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServiceAllowed(t *testing.T) {
	patterns := []string{"stack-*.service", "docker.service"}
	assert.NoError(t, serviceAllowed("stack-web.service", patterns))
	assert.NoError(t, serviceAllowed("docker.service", patterns))
	// the hub reports services without the suffix
	assert.NoError(t, serviceAllowed("stack-web", patterns))
	assert.NoError(t, serviceAllowed("docker", patterns))
	assert.ErrorContains(t, serviceAllowed("sshd", patterns), "SERVICE_PATTERNS")
	assert.Equal(t, "stack-web.service", withServiceSuffix("stack-web"))
	assert.Equal(t, "stack-web.service", withServiceSuffix("stack-web.service"))

	assert.ErrorContains(t, serviceAllowed("sshd.service", patterns), "SERVICE_PATTERNS")
	assert.ErrorContains(t, serviceAllowed("stack-web.timer", patterns), "invalid")
	assert.ErrorContains(t, serviceAllowed("stack-web.socket", []string{"*"}), "invalid")
	assert.ErrorContains(t, serviceAllowed("../stack-web.service", patterns), "invalid")
	assert.ErrorContains(t, serviceAllowed("stack web.service", patterns), "invalid")
	assert.ErrorContains(t, serviceAllowed("", patterns), "invalid")
	assert.ErrorContains(t, serviceAllowed("stack-web.service;reboot.service", patterns), "invalid")

	// the agent never controls itself, even when a pattern would match it
	assert.ErrorContains(t, serviceAllowed("beszel-agent.service", []string{"*.service"}), "own service")
	assert.ErrorContains(t, serviceAllowed("beszel-agent-nvidia.service", []string{"*.service"}), "own service")
	// the default pattern allows every other service
	assert.NoError(t, serviceAllowed("nginx.service", []string{"*.service"}))
}
