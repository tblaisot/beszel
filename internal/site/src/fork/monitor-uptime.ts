import type { RawMonitorStatsRecord } from "@/types"

export const UPTIME_SEGMENTS = 48
export const UPTIME_SEGMENT_MS = 30 * 60_000

export interface UptimeSegment {
	/** Segment start, unix ms */
	start: number
	ok: number
	total: number
}

export interface MonitorUptime {
	segments: UptimeSegment[]
	/** Availability over the whole window in percent, or null when no checks were recorded */
	pct: number | null
}

type UptimeRecord = Pick<RawMonitorStatsRecord, "monitor" | "type" | "total_count" | "success_count" | "created">

/**
 * Builds the uptime history of each monitor from stored stats records (success_count / total_count).
 * Segments are aligned to `now`. Recent 1m records are exact, so they win over the coarser
 * longer-tier records wherever both cover the same segment (they never double count).
 */
export function buildMonitorUptime(
	records: UptimeRecord[],
	now: number,
	segmentCount = UPTIME_SEGMENTS,
	segmentMs = UPTIME_SEGMENT_MS
): Map<string, MonitorUptime> {
	const windowStart = now - segmentCount * segmentMs
	const fine = new Map<string, { ok: number; total: number }[]>()
	const coarse = new Map<string, { ok: number; total: number }[]>()

	for (const rec of records) {
		if (rec.created <= windowStart || rec.created > now + segmentMs) continue
		const index = Math.min(segmentCount - 1, Math.floor((rec.created - windowStart) / segmentMs))
		const target = rec.type === "1m" ? fine : coarse
		let buckets = target.get(rec.monitor)
		if (!buckets) {
			buckets = Array.from({ length: segmentCount }, () => ({ ok: 0, total: 0 }))
			target.set(rec.monitor, buckets)
		}
		buckets[index].ok += rec.success_count
		buckets[index].total += rec.total_count
	}

	const result = new Map<string, MonitorUptime>()
	for (const monitor of new Set([...fine.keys(), ...coarse.keys()])) {
		let ok = 0
		let total = 0
		const segments = Array.from({ length: segmentCount }, (_, i): UptimeSegment => {
			const exact = fine.get(monitor)?.[i]
			const chosen = exact && exact.total > 0 ? exact : (coarse.get(monitor)?.[i] ?? { ok: 0, total: 0 })
			ok += chosen.ok
			total += chosen.total
			return { start: windowStart + i * segmentMs, ok: chosen.ok, total: chosen.total }
		})
		result.set(monitor, { segments, pct: total > 0 ? (ok * 100) / total : null })
	}
	return result
}

export type SegmentState = "none" | "up" | "partial" | "down"

export function segmentState({ ok, total }: Pick<UptimeSegment, "ok" | "total">): SegmentState {
	if (total === 0) return "none"
	if (ok === total) return "up"
	return ok === 0 ? "down" : "partial"
}

/** Percentage text with one decimal, never rounding a partial outage up to 100 */
export function formatUptimePct(pct: number | null) {
	if (pct === null) return "-"
	if (pct >= 100) return "100%"
	return `${Math.min(99.9, Math.floor(pct * 10) / 10).toFixed(1)}%`
}
