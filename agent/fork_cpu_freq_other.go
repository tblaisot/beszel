//go:build !linux

package agent

import "github.com/henrygd/beszel/internal/entities/system"

// cpufreq sysfs is Linux-specific.
func (a *Agent) updateCpuFreq(_ *system.Stats) {}
