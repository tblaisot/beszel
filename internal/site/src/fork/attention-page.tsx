import { useLingui } from "@lingui/react/macro"
import { useStore } from "@nanostores/react"
import { getPagePath } from "@nanostores/router"
import { CheckCircle2Icon, ChevronRightIcon, TriangleAlertIcon } from "lucide-react"
import { memo, useEffect } from "react"
import { FooterRepoLink } from "@/components/footer-repo-link"
import { $router, Link } from "@/components/router"
import { Badge } from "@/components/ui/badge"
import { Card, CardHeader, CardTitle } from "@/components/ui/card"
import { $allSystemsById } from "@/lib/stores"
import { cn, decimalString, formatBytes, formatMicroseconds } from "@/lib/utils"
import { type AttentionItem, type AttentionSeverity, type AttentionType, useAttention } from "./use-attention"

const SEVERITY_DOT: Record<AttentionSeverity, string> = {
	critical: "bg-red-500",
	warning: "bg-yellow-500",
	info: "bg-blue-500",
}

const TYPE_LABEL: Record<AttentionType, string> = {
	system: "SYSTEM",
	monitor: "MONITOR",
	alert: "ALERT",
	zfs: "ZFS",
	smart: "S.M.A.R.T.",
	unit: "SERVICE",
	container: "CONTAINER",
	update: "UPDATE",
}

/** Where an item leads: the system page for system-level problems, the matching list otherwise. */
function itemHref(item: AttentionItem) {
	switch (item.type) {
		case "monitor":
			return getPagePath($router, "monitors")
		case "smart":
			return getPagePath($router, "smart")
		case "container":
		case "update":
			return getPagePath($router, "containers")
		default:
			return getPagePath($router, "system", { id: item.system })
	}
}

const relativeTime = new Intl.RelativeTimeFormat(undefined, { numeric: "auto", style: "narrow" })

function since(value?: string) {
	if (!value) return ""
	const ms = Date.parse(value.replace(" ", "T"))
	if (Number.isNaN(ms)) return ""
	const minutes = Math.round((ms - Date.now()) / 60_000)
	if (Math.abs(minutes) < 60) return relativeTime.format(minutes, "minute")
	const hours = Math.round(minutes / 60)
	if (Math.abs(hours) < 48) return relativeTime.format(hours, "hour")
	return relativeTime.format(Math.round(hours / 24), "day")
}

function Tile({ title, value, detail }: { title: string; value: string; detail?: string }) {
	return (
		<div className="px-4 py-3 border-s first:border-s-0 max-sm:border-s-0 max-sm:border-t first:max-sm:border-t-0">
			<div className="text-xs uppercase tracking-wide text-muted-foreground">{title}</div>
			<div className="text-2xl font-semibold tabular-nums">{value}</div>
			{detail && <div className="text-xs text-muted-foreground tabular-nums">{detail}</div>}
		</div>
	)
}

function formatSize(bytes: number) {
	const { value, unit } = formatBytes(bytes, false, undefined, false)
	return `${decimalString(value, value >= 10 ? 1 : 2)} ${unit}`
}

/** Home view that answers "is everything OK?": open problems first, then a few overview numbers. */
export default memo(function AttentionPage() {
	const { t } = useLingui()
	const data = useAttention()
	const systems = useStore($allSystemsById)

	useEffect(() => {
		document.title = `${t`Needs attention`} / Beszel`
	}, [t])

	const counts = data?.counts
	const problems = counts ? counts.critical + counts.warning : 0
	const tone = !counts ? "muted" : counts.critical > 0 ? "critical" : counts.warning > 0 ? "warning" : "ok"
	const overview = data?.overview

	return (
		<>
			<div className="grid gap-4 mb-4">
				<Card
					className={cn("px-4 py-4 sm:px-6", {
						"border-red-500/40 bg-red-500/5": tone === "critical",
						"border-yellow-500/40 bg-yellow-500/5": tone === "warning",
						"border-green-500/40 bg-green-500/5": tone === "ok",
					})}
				>
					<div className="flex flex-wrap items-center gap-4">
						{tone === "ok" ? (
							<CheckCircle2Icon className="size-8 text-green-500" />
						) : (
							<TriangleAlertIcon className={cn("size-8", tone === "critical" ? "text-red-500" : "text-yellow-500")} />
						)}
						<div className="me-auto">
							<h1 className="text-xl font-semibold">
								{!counts ? t`Loading...` : problems ? t`${problems} need your attention` : t`Everything is OK`}
							</h1>
							{overview && (
								<p className="text-sm text-muted-foreground">
									{t`${overview.systemsUp} of ${overview.systemsTotal} systems up`}
								</p>
							)}
						</div>
						{counts && (
							<div className="flex gap-2">
								{(["critical", "warning", "info"] as AttentionSeverity[]).map((severity) => (
									<div key={severity} className="min-w-16 rounded-md border px-3 py-1.5 text-center">
										<div className="text-lg font-semibold tabular-nums">{counts[severity]}</div>
										<div className="text-[10px] uppercase tracking-wide text-muted-foreground">
											{severity === "critical" ? t`Critical` : severity === "warning" ? t`Warning` : t`Info`}
										</div>
									</div>
								))}
							</div>
						)}
					</div>
				</Card>

				{!!data?.items.length && (
					<Card className="px-3 py-4 sm:px-6">
						<CardHeader className="p-0 mb-3">
							<CardTitle className="text-sm uppercase tracking-wide text-muted-foreground">
								{t`Needs attention`} <Badge variant="outline">{data.items.length}</Badge>
							</CardTitle>
						</CardHeader>
						<ul className="divide-y">
							{data.items.map((item) => (
								<li key={`${item.type}:${item.system}:${item.name}:${item.reason}`}>
									<Link
										href={itemHref(item)}
										className="flex items-center gap-3 py-2.5 px-1 hover:bg-muted/50 rounded-sm text-sm"
									>
										<span className={cn("size-2.5 shrink-0 rounded-full", SEVERITY_DOT[item.severity])} />
										<span className="font-medium truncate">{item.name}</span>
										<Badge variant="outline" className="text-[10px] shrink-0">
											{TYPE_LABEL[item.type]}
										</Badge>
										<span className="text-muted-foreground truncate">
											{item.reason}
											{systems[item.system] && item.type !== "system" && ` · ${systems[item.system].name}`}
										</span>
										<span className="ms-auto text-xs text-muted-foreground tabular-nums shrink-0">
											{since(item.since)}
										</span>
										<ChevronRightIcon className="size-4 shrink-0 text-muted-foreground" />
									</Link>
								</li>
							))}
						</ul>
					</Card>
				)}

				{overview && (
					<Card className="overflow-hidden">
						<div className="grid sm:grid-cols-2 lg:grid-cols-4">
							<Tile
								title={t`Monitors`}
								value={
									overview.monitorsTotal
										? `${decimalString((overview.monitorsUp * 100) / overview.monitorsTotal, 1)}%`
										: "-"
								}
								detail={`${overview.monitorsUp} / ${overview.monitorsTotal} ${t`up`}`}
							/>
							<Tile
								title={t`Response time`}
								value={overview.avgResponseUs ? formatMicroseconds(overview.avgResponseUs, false) : "-"}
								detail={t`average of monitors that are up`}
							/>
							<Tile title={t`Systems`} value={`${overview.systemsUp} / ${overview.systemsTotal}`} detail={t`up`} />
							<Tile title={t`Updates`} value={String(overview.updates)} detail={t`container image updates available`} />
						</div>
					</Card>
				)}

				{!!overview?.hosts.length && (
					<Card className="px-4 py-4 sm:px-6">
						<CardTitle className="text-sm uppercase tracking-wide text-muted-foreground mb-3">{t`Hosts`}</CardTitle>
						<div className="grid gap-3 sm:grid-cols-2">
							{overview.hosts.map((host) => (
								<Link
									key={host.id}
									href={getPagePath($router, "system", { id: host.id })}
									className="grid gap-1 text-sm"
								>
									<span className="font-medium">{host.name}</span>
									<span className="text-muted-foreground tabular-nums">
										CPU {decimalString((host.cpu * host.cores) / 100, 2)} / {host.cores} cores · {t`Memory`}{" "}
										{formatSize((host.mp * host.memory) / 100)} / {formatSize(host.memory)}
									</span>
								</Link>
							))}
						</div>
					</Card>
				)}

				{!!overview?.pools.length && (
					<Card className="px-4 py-4 sm:px-6">
						<CardTitle className="text-sm uppercase tracking-wide text-muted-foreground mb-3">{t`Storage pools`}</CardTitle>
						<ul className="grid gap-2">
							{overview.pools.map((pool) => (
								<li
									key={`${pool.system}:${pool.name}`}
									className="grid grid-cols-[6rem_1fr_9rem_5rem] items-center gap-3 text-sm"
								>
									<span className="font-medium truncate">{pool.name}</span>
									<span className="h-2.5 rounded-sm bg-muted overflow-hidden">
										<span
											className={cn(
												"block h-full",
												pool.usedPct >= 90 ? "bg-red-500" : pool.usedPct >= 80 ? "bg-yellow-500" : "bg-green-500"
											)}
											style={{ width: `${Math.min(100, pool.usedPct)}%` }}
										/>
									</span>
									<span className="text-muted-foreground tabular-nums text-end">
										{decimalString(pool.usedPct, 1)}% · {formatSize(pool.size)}
									</span>
									<Badge
										variant={pool.health === "ONLINE" ? "outline" : "destructive"}
										className="justify-self-end text-[10px]"
									>
										{pool.health}
									</Badge>
								</li>
							))}
						</ul>
					</Card>
				)}
			</div>
			<FooterRepoLink />
		</>
	)
})
