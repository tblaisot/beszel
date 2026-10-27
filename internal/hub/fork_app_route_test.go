//go:build testing

package hub

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Fork routes must be known to the server, or a direct load answers 404.
func TestIsAppRouteForkRoutes(t *testing.T) {
	assert.True(t, isAppRoute("/attention", "/"))
	assert.True(t, isAppRoute("/attention/", "/"))
	assert.True(t, isAppRoute("/beszel/attention", "/beszel/"))
	assert.False(t, isAppRoute("/attention/extra", "/"))
}
