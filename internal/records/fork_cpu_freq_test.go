//go:build testing

package records

import (
	"testing"

	"github.com/henrygd/beszel/internal/entities/system"
	"github.com/stretchr/testify/assert"
)

func TestCpuFreqAverager(t *testing.T) {
	var c cpuFreqAverager
	assert.Equal(t, [3]uint16{}, c.result())

	c.add([3]uint16{}) // records without cpufreq are skipped
	c.add([3]uint16{2000, 3000, 3800})
	c.add([3]uint16{3000, 3400, 3800})
	assert.Equal(t, [3]uint16{2500, 3400, 3800}, c.result())
}

func TestAverageSystemStatsKeepsCpuFreq(t *testing.T) {
	result := AverageSystemStatsSlice([]system.Stats{
		{CpuFreq: [3]uint16{1000, 1500, 3800}},
		{CpuFreq: [3]uint16{3000, 3600, 3800}},
		{}, // an agent without cpufreq must not drag the average down
	})
	assert.Equal(t, [3]uint16{2000, 3600, 3800}, result.CpuFreq)
}
