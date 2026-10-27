import { expect, test } from "bun:test"
import { buildMonitorUptime, formatUptimePct, segmentState } from "./monitor-uptime"

const MIN = 60_000
const now = 1_000_000 * MIN
const seg = 30 * MIN

const rec = (monitor: string, type: string, agoMin: number, ok: number, total: number) => ({
	monitor,
	type,
	created: now - agoMin * MIN,
	success_count: ok,
	total_count: total,
})

test("all successes give 100% and green segments", () => {
	const map = buildMonitorUptime([rec("a", "20m", 100, 20, 20), rec("a", "20m", 500, 20, 20)], now)
	const up = map.get("a")
	expect(up?.pct).toBe(100)
	expect(up?.segments.filter((s) => s.total > 0).every((s) => segmentState(s) === "up")).toBe(true)
})

test("a failure lowers the percentage and marks the segment", () => {
	const map = buildMonitorUptime([rec("a", "20m", 100, 15, 20), rec("a", "20m", 500, 20, 20)], now)
	const up = map.get("a")
	expect(up?.pct).toBeCloseTo(87.5)
	expect(up?.segments.map(segmentState).filter((s) => s === "partial")).toHaveLength(1)
})

test("a fully failed segment is down", () => {
	const map = buildMonitorUptime([rec("a", "1m", 10, 0, 5)], now)
	expect(segmentState(map.get("a")?.segments.at(-1) ?? { ok: 0, total: 0 })).toBe("down")
	expect(map.get("a")?.pct).toBe(0)
})

test("1m records win over 20m records in the same segment", () => {
	const map = buildMonitorUptime([rec("a", "1m", 5, 10, 10), rec("a", "20m", 5, 0, 20)], now)
	const last = map.get("a")?.segments.at(-1)
	expect(last).toMatchObject({ ok: 10, total: 10 })
})

test("records outside the window are ignored and empty monitors are absent", () => {
	const map = buildMonitorUptime([rec("a", "20m", 24 * 60 + 40, 1, 1)], now)
	expect(map.has("a")).toBe(false)
})

test("monitors are kept apart", () => {
	const map = buildMonitorUptime([rec("a", "20m", 30, 0, 10), rec("b", "20m", 30, 10, 10)], now)
	expect(map.get("a")?.pct).toBe(0)
	expect(map.get("b")?.pct).toBe(100)
	expect(seg).toBe(30 * MIN)
})

test("formatUptimePct never rounds an outage up to 100", () => {
	expect(formatUptimePct(null)).toBe("-")
	expect(formatUptimePct(100)).toBe("100%")
	expect(formatUptimePct(99.97)).toBe("99.9%")
	expect(formatUptimePct(87.5)).toBe("87.5%")
})
