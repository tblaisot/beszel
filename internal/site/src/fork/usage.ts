/**
 * CPU and memory shown as an absolute value or as a share of the host, instead of a share of the
 * system's own (often small) limit. Pure helpers; the stores live in use-usage.ts.
 */

export type UsageMode = "machine" | "absolute" | "host"

export const USAGE_MODES: UsageMode[] = ["machine", "absolute", "host"]

export interface Capacity {
	/** CPU cores of the system */
	cores: number
	/** Total memory in bytes */
	memory: number
	/** Id of the host system this one runs on, if linked */
	parent?: string
}

export type CapacityMap = Record<string, Capacity>

export type UsageKind = "sys-cpu" | "sys-mem" | "ct-cpu" | "ct-mem"

export type UsageUnit = "pct" | "cores" | "bytes"

export interface UsageValue {
	value: number
	unit: UsageUnit
}

const MIB = 1024 * 1024

/** The capacity that "% of host" is measured against: the linked parent, else the system itself. */
export function hostCapacity(caps: CapacityMap, systemId: string): Capacity | undefined {
	const own = caps[systemId]
	const parent = own?.parent ? caps[own.parent] : undefined
	return parent ?? own
}

/**
 * Converts a reported figure into the chosen mode.
 * - sys-cpu / ct-cpu: raw is a percentage of the system's CPU
 * - sys-mem: raw is a percentage of the system's memory
 * - ct-mem: raw is the container's memory in MiB
 * Returns undefined when the capacity needed is unknown, so callers can fall back to the default view.
 */
export function usageValue(
	kind: UsageKind,
	mode: UsageMode,
	raw: number,
	systemId: string,
	caps: CapacityMap
): UsageValue | undefined {
	const cap = caps[systemId]
	const isCpu = kind === "sys-cpu" || kind === "ct-cpu"

	if (mode === "machine") {
		if (kind === "ct-mem") return { value: raw * MIB, unit: "bytes" }
		return { value: raw, unit: "pct" }
	}

	if (isCpu) {
		if (!cap?.cores) return undefined
		const cores = (raw * cap.cores) / 100
		if (mode === "absolute") return { value: cores, unit: "cores" }
		const host = hostCapacity(caps, systemId)
		return host?.cores ? { value: (cores * 100) / host.cores, unit: "pct" } : undefined
	}

	let bytes: number
	if (kind === "ct-mem") {
		bytes = raw * MIB
	} else {
		if (!cap?.memory) return undefined
		bytes = (raw * cap.memory) / 100
	}
	if (mode === "absolute") return { value: bytes, unit: "bytes" }
	const host = hostCapacity(caps, systemId)
	return host?.memory ? { value: (bytes * 100) / host.memory, unit: "pct" } : undefined
}
