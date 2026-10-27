//go:build linux && testing

package agent

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func writeCpuFreq(t *testing.T, root, cpu string, files map[string]string) {
	t.Helper()
	dir := filepath.Join(root, cpu, "cpufreq")
	assert.NoError(t, os.MkdirAll(dir, 0o755))
	for name, value := range files {
		assert.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte(value+"\n"), 0o644))
	}
}

func TestReadCpuFreq(t *testing.T) {
	root := t.TempDir()
	// intel_pstate style: scaling_cur_freq and cpuinfo_max_freq, no cpuinfo_cur_freq
	writeCpuFreq(t, root, "cpu0", map[string]string{"scaling_cur_freq": "3000000", "cpuinfo_max_freq": "3800000"})
	writeCpuFreq(t, root, "cpu1", map[string]string{"scaling_cur_freq": "1000000", "cpuinfo_max_freq": "3800000"})
	// falls back to cpuinfo_avg_freq when scaling_cur_freq is missing
	writeCpuFreq(t, root, "cpu2", map[string]string{"cpuinfo_avg_freq": "2000000", "cpuinfo_max_freq": "3700000"})
	// entries that are not CPUs are ignored
	assert.NoError(t, os.MkdirAll(filepath.Join(root, "cpufreq", "policy0"), 0o755))

	freq, ok := readCpuFreq(root)
	assert.True(t, ok)
	assert.Equal(t, [3]uint16{2000, 3000, 3800}, freq)
}

func TestReadCpuFreqUnavailable(t *testing.T) {
	// no cpufreq at all (typical for VMs)
	root := t.TempDir()
	assert.NoError(t, os.MkdirAll(filepath.Join(root, "cpu0"), 0o755))
	_, ok := readCpuFreq(root)
	assert.False(t, ok)

	// cpufreq directory without readable or valid values
	writeCpuFreq(t, root, "cpu1", map[string]string{"scaling_cur_freq": "not-a-number"})
	_, ok = readCpuFreq(root)
	assert.False(t, ok)

	_, ok = readCpuFreq(filepath.Join(root, "missing"))
	assert.False(t, ok)
}
