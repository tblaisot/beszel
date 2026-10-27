//go:build linux

package agent

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/henrygd/beszel/internal/entities/system"
)

// cpuSysfsRoot holds one cpuN directory per CPU; cpuN/cpufreq exists when frequency scaling is exposed.
const cpuSysfsRoot = "/sys/devices/system/cpu"

// updateCpuFreq sets systemStats.CpuFreq from cpufreq sysfs. It does nothing when the host exposes
// no cpufreq (most VMs and containers), so the stat is omitted and no chart is shown.
func (a *Agent) updateCpuFreq(systemStats *system.Stats) {
	if freq, ok := readCpuFreq(cpuSysfsRoot); ok {
		systemStats.CpuFreq = freq
	}
}

// readCpuFreq returns [average MHz, highest MHz, ceiling MHz] across CPUs.
// The current frequency is scaling_cur_freq, falling back to cpuinfo_avg_freq: cpuinfo_cur_freq
// is privileged and missing with intel_pstate, so it is never used. The ceiling is the largest
// cpuinfo_max_freq. Values are kHz in sysfs.
func readCpuFreq(root string) (freq [3]uint16, ok bool) {
	dirs, err := filepath.Glob(filepath.Join(root, "cpu[0-9]*", "cpufreq"))
	if err != nil || len(dirs) == 0 {
		return freq, false
	}
	var sum, peak, ceiling, count uint64
	for _, dir := range dirs {
		cur, found := readKHz(filepath.Join(dir, "scaling_cur_freq"))
		if !found {
			cur, found = readKHz(filepath.Join(dir, "cpuinfo_avg_freq"))
		}
		if !found {
			continue
		}
		sum += cur
		count++
		peak = max(peak, cur)
		if limit, found := readKHz(filepath.Join(dir, "cpuinfo_max_freq")); found {
			ceiling = max(ceiling, limit)
		}
	}
	if count == 0 {
		return freq, false
	}
	toMHz := func(khz uint64) uint16 { return uint16(min(khz/1000, 65535)) }
	return [3]uint16{toMHz(sum / count), toMHz(peak), toMHz(ceiling)}, true
}

func readKHz(path string) (uint64, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	return value, err == nil
}
