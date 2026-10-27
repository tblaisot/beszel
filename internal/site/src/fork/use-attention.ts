import { useStore } from "@nanostores/react"
import { atom } from "nanostores"
import { useEffect } from "react"
import { pb } from "@/lib/api"

export type AttentionSeverity = "critical" | "warning" | "info"
export type AttentionType = "system" | "monitor" | "alert" | "zfs" | "smart" | "unit" | "container" | "update"

export interface AttentionItem {
	type: AttentionType
	severity: AttentionSeverity
	/** system id */
	system: string
	name: string
	reason: string
	since?: string
}

export interface AttentionData {
	items: AttentionItem[]
	counts: Record<AttentionSeverity, number>
	overview: {
		systemsUp: number
		systemsTotal: number
		monitorsUp: number
		monitorsTotal: number
		/** average response of monitors that are up, in microseconds */
		avgResponseUs: number
		updates: number
		hosts: { id: string; name: string; cpu: number; mp: number; cores: number; memory: number }[]
		pools: { system: string; name: string; health: string; usedPct: number; size: number }[]
	}
}

const REFRESH_MS = 60_000

export const $attention = atom<AttentionData | null>(null)

let subscribers = 0
let timer: ReturnType<typeof setInterval> | undefined

function load() {
	if (document.hidden || !pb.authStore.isValid) return
	pb.send<AttentionData>("/api/beszel/fork/attention", { requestKey: null })
		.then((data) => $attention.set(data))
		.catch((error) => console.error("Failed to fetch attention items:", error))
}

/**
 * What needs attention, shared by the nav badge and the page. One request a minute while the page
 * is visible, and none when nothing is subscribed.
 */
export function useAttention() {
	const data = useStore($attention)
	useEffect(() => {
		if (subscribers++ === 0) {
			load()
			timer = setInterval(load, REFRESH_MS)
			document.addEventListener("visibilitychange", load)
		}
		return () => {
			if (--subscribers === 0) {
				clearInterval(timer)
				document.removeEventListener("visibilitychange", load)
			}
		}
	}, [])
	return data
}
