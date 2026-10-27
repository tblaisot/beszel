import { useStore } from "@nanostores/react"
import { atom } from "nanostores"
import { useEffect } from "react"
import { pb } from "@/lib/api"
import type { RawMonitorStatsRecord } from "@/types"
import { buildMonitorUptime, type MonitorUptime, UPTIME_SEGMENTS, UPTIME_SEGMENT_MS } from "./monitor-uptime"

const REFRESH_MS = 60_000

/**
 * Latest uptime per monitor id. Table rows are memoized, so cells subscribe to this store
 * themselves instead of relying on the column definitions being rebuilt.
 */
export const $monitorUptime = atom<Map<string, MonitorUptime>>(new Map())

async function fetchUptime(monitorIds?: string[]) {
	const now = Date.now()
	const params: Record<string, string | number> = {
		from: now - UPTIME_SEGMENTS * UPTIME_SEGMENT_MS,
		// The newest segment is fully covered by 1m records, so older 1m records would only add rows.
		recent: now - UPTIME_SEGMENT_MS,
	}
	let filter = `created>{:from} && (type="20m" || (type="1m" && created>{:recent}))`
	if (monitorIds?.length) {
		monitorIds.forEach((id, i) => {
			params[`m${i}`] = id
		})
		filter += ` && (${monitorIds.map((_, i) => `monitor={:m${i}}`).join(" || ")})`
	}
	// One query for every row: 20m records cover the 24h window, 1m records make the newest segment exact.
	const records = await pb.collection<RawMonitorStatsRecord>("network_monitor_stats").getFullList({
		batch: 1000,
		// the SDK would otherwise cancel this when another request to the same collection is in flight
		requestKey: null,
		filter: pb.filter(filter, params),
		fields: "monitor,type,total_count,success_count,created",
	})
	return buildMonitorUptime(records, now)
}

/** Uptime history for the monitors table. Refreshes once a minute while the page is visible. */
export function useMonitorUptime(monitorIds: string[]) {
	// Stable key so a new array with the same ids doesn't restart the fetch loop.
	const key = [...monitorIds].sort().join(",")

	useEffect(() => {
		if (!key) return
		let cancelled = false
		const load = () => {
			if (document.hidden) return
			fetchUptime(key.split(","))
				.then((next) => !cancelled && $monitorUptime.set(next))
				.catch((error) => console.error("Failed to fetch monitor uptime:", error))
		}
		load()
		const timer = setInterval(load, REFRESH_MS)
		document.addEventListener("visibilitychange", load)
		return () => {
			cancelled = true
			clearInterval(timer)
			document.removeEventListener("visibilitychange", load)
		}
	}, [key])

	return useStore($monitorUptime)
}
