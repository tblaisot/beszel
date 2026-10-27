import { useStore } from "@nanostores/react"
import { atom } from "nanostores"
import { useEffect } from "react"
import { pb } from "@/lib/api"
import type { SystemDetailsRecord } from "@/types"
import { type CapacityMap, type UsageMode, USAGE_MODES } from "./usage"

const MODE_KEY = "besz-fork-usage-mode"

function readMode(): UsageMode {
	try {
		const stored = localStorage.getItem(MODE_KEY) as UsageMode | null
		if (stored && USAGE_MODES.includes(stored)) return stored
	} catch {}
	return "machine"
}

/** How CPU and memory are shown in the systems and containers tables. */
export const $usageMode = atom<UsageMode>(readMode())

$usageMode.listen((mode) => {
	try {
		localStorage.setItem(MODE_KEY, mode)
	} catch {}
})

/** Cores, total memory and parent host of every system. `version` changes on each load. */
export const $capacity = atom<{ version: number; map: CapacityMap }>({ version: 0, map: {} })

let loading: Promise<void> | undefined

/** Loads capacities once (or again when forced, e.g. after the parent host is edited). */
export function loadCapacity(force = false) {
	if (loading && !force) return loading
	loading = (async () => {
		try {
			const [details, systems] = await Promise.all([
				pb
					.collection<SystemDetailsRecord>("system_details")
					.getFullList({ fields: "system,cores,memory", batch: 500, requestKey: null }),
				pb
					.collection<{ id: string; parent?: string }>("systems")
					.getFullList({ fields: "id,parent", batch: 500, requestKey: null }),
			])
			const map: CapacityMap = {}
			for (const d of details) {
				map[d.system] = { cores: d.cores, memory: d.memory }
			}
			for (const s of systems) {
				if (s.parent && map[s.id]) map[s.id].parent = s.parent
			}
			$capacity.set({ version: $capacity.get().version + 1, map })
		} catch (error) {
			console.error("Failed to load system capacities:", error)
			loading = undefined
		}
	})()
	return loading
}

/** Current mode and capacities for table columns. Triggers the capacity load on first use. */
export function useUsage() {
	const mode = useStore($usageMode)
	const { map: caps } = useStore($capacity)
	useEffect(() => {
		loadCapacity()
	}, [])
	return { mode, caps }
}
