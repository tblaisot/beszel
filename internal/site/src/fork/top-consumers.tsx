import { useLingui } from "@lingui/react/macro"
import { useStore } from "@nanostores/react"
import { ChevronDownIcon, ChevronUpIcon } from "lucide-react"
import { useEffect, useState } from "react"
import { Button } from "@/components/ui/button"
import { Card, CardHeader, CardTitle } from "@/components/ui/card"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { pb } from "@/lib/api"
import { $allSystemsById } from "@/lib/stores"
import { chartTimeData, cn } from "@/lib/utils"
import { formatUsage } from "./usage-columns"
import { usageValue } from "./usage"
import { $capacity, $usageMode, useUsage } from "./use-usage"

type Metric = "cpu" | "memory"
type Window = "1h" | "12h" | "24h" | "1w" | "30d"

const WINDOWS: Window[] = ["1h", "12h", "24h", "1w", "30d"]
const REFRESH_MS = 60_000
const PREFS_KEY = "besz-fork-top-consumers"

interface TopConsumer {
	/** system id */
	s: string
	/** container name */
	n: string
	/** average cpu, percent of the system */
	c: number
	/** average memory, MiB */
	m: number
}

function readPrefs(): { metric: Metric; window: Window; open: boolean } {
	try {
		const stored = JSON.parse(localStorage.getItem(PREFS_KEY) || "{}")
		return {
			metric: stored.metric === "memory" ? "memory" : "cpu",
			window: WINDOWS.includes(stored.window) ? stored.window : "1h",
			open: stored.open !== false,
		}
	} catch {
		return { metric: "cpu", window: "1h", open: true }
	}
}

/**
 * Ranked bars of the containers using the most CPU or memory across all systems.
 * The hub ranks by absolute usage (cores, MiB), so the order is comparable between systems of
 * different sizes. The default display mode shows absolute values to match that order.
 */
export function TopConsumers() {
	const { t } = useLingui()
	const [prefs, setPrefs] = useState(readPrefs)
	const [items, setItems] = useState<TopConsumer[]>([])
	const [loaded, setLoaded] = useState(false)
	const systems = useStore($allSystemsById)
	const mode = useStore($usageMode)
	const { map: caps } = useStore($capacity)
	useUsage()

	const update = (next: Partial<typeof prefs>) => {
		const merged = { ...prefs, ...next }
		setPrefs(merged)
		try {
			localStorage.setItem(PREFS_KEY, JSON.stringify(merged))
		} catch {}
	}

	useEffect(() => {
		if (!prefs.open) return
		let cancelled = false
		const load = () => {
			if (document.hidden) return
			pb.send<{ items: TopConsumer[] }>("/api/beszel/fork/top-consumers", {
				query: { metric: prefs.metric, window: prefs.window, limit: 10 },
				requestKey: null,
			})
				.then((res) => {
					if (cancelled) return
					setItems(res.items)
					setLoaded(true)
				})
				.catch((error) => console.error("Failed to fetch top consumers:", error))
		}
		load()
		const timer = setInterval(load, REFRESH_MS)
		document.addEventListener("visibilitychange", load)
		return () => {
			cancelled = true
			clearInterval(timer)
			document.removeEventListener("visibilitychange", load)
		}
	}, [prefs.metric, prefs.window, prefs.open])

	const kind = prefs.metric === "cpu" ? "ct-cpu" : "ct-mem"
	const displayMode = mode === "machine" ? "absolute" : mode
	const rows = items.map((item) => {
		const raw = prefs.metric === "cpu" ? item.c : item.m
		const usage = usageValue(kind, displayMode, raw, item.s, caps) ?? usageValue(kind, "machine", raw, item.s, caps)
		return { item, usage }
	})
	const max = Math.max(...rows.map((row) => row.usage?.value ?? 0), 0)

	return (
		<Card className="w-full px-3 py-5 sm:py-6 sm:px-6 mb-4">
			<CardHeader className="p-0 mb-0">
				<div className="flex flex-wrap items-center gap-x-4 gap-y-3">
					<CardTitle className="me-auto px-2 sm:px-1">{t`Top consumers`}</CardTitle>
					{prefs.open && (
						<>
							<div className="flex gap-1">
								{(["cpu", "memory"] as Metric[]).map((metric) => (
									<Button
										key={metric}
										size="sm"
										variant={prefs.metric === metric ? "default" : "outline"}
										onClick={() => update({ metric })}
									>
										{metric === "cpu" ? t`CPU` : t`Memory`}
									</Button>
								))}
							</div>
							<Select value={prefs.window} onValueChange={(value) => update({ window: value as Window })}>
								<SelectTrigger className="w-auto gap-2 h-9" aria-label={t`Time period`}>
									<SelectValue />
								</SelectTrigger>
								<SelectContent>
									{WINDOWS.map((window) => (
										<SelectItem key={window} value={window}>
											{chartTimeData[window].label()}
										</SelectItem>
									))}
								</SelectContent>
							</Select>
						</>
					)}
					<Button
						size="icon"
						variant="ghost"
						aria-label={prefs.open ? t`Collapse` : t`Expand`}
						onClick={() => update({ open: !prefs.open })}
					>
						{prefs.open ? <ChevronUpIcon className="size-4" /> : <ChevronDownIcon className="size-4" />}
					</Button>
				</div>
			</CardHeader>
			{prefs.open && (
				<ol className="mt-4 grid gap-1.5">
					{rows.map(({ item, usage }, index) => (
						<li
							key={`${item.s}:${item.n}`}
							className="grid grid-cols-[1.25rem_minmax(0,14rem)_1fr_6rem] items-center gap-x-3 text-sm"
						>
							<span className="text-muted-foreground tabular-nums text-end">{index + 1}</span>
							<span className="truncate" title={item.n}>
								{item.n}
								<span className="ms-2 text-xs text-muted-foreground">{systems[item.s]?.name}</span>
							</span>
							<span className="h-2.5 rounded-sm bg-muted overflow-hidden">
								<span
									className={cn("block h-full rounded-sm bg-primary/70")}
									style={{ width: `${max > 0 && usage ? Math.max(1, (usage.value / max) * 100) : 0}%` }}
								/>
							</span>
							<span className="tabular-nums text-end">{usage ? formatUsage(usage) : "-"}</span>
						</li>
					))}
					{loaded && !rows.length && <li className="text-sm text-muted-foreground px-1">-</li>}
				</ol>
			)}
		</Card>
	)
}
