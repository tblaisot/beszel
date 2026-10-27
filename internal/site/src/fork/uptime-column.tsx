import type { ColumnDef } from "@tanstack/react-table"
import { t } from "@lingui/core/macro"
import { ActivityIcon } from "lucide-react"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import type { NetworkMonitorRecord } from "@/types"
import { useStore } from "@nanostores/react"
import { $monitorUptime } from "./use-monitor-uptime"
import { formatUptimePct, type MonitorUptime, segmentState, type SegmentState } from "./monitor-uptime"

const segmentColors: Record<SegmentState, string> = {
	none: "bg-muted-foreground/20",
	up: "bg-green-500",
	partial: "bg-yellow-500",
	down: "bg-red-500",
}

const timeFormat = new Intl.DateTimeFormat(undefined, { hour: "2-digit", minute: "2-digit" })

/** Reads its monitor's uptime from the store, so memoized rows still update when data arrives. */
function UptimeCell({ monitor }: { monitor: NetworkMonitorRecord }) {
	return <UptimeBar uptime={useStore($monitorUptime).get(monitor.id)} muted={!monitor.enabled} />
}

/** One bar per time segment (green up, yellow some failures, red down, grey no data). */
export function UptimeBar({ uptime, muted }: { uptime?: MonitorUptime; muted?: boolean }) {
	return (
		<div className={cn("flex items-center gap-2 ms-1.5", muted && "opacity-50")}>
			<div className="flex h-5 gap-px">
				{(uptime?.segments ?? []).map((segment) => (
					<span
						key={segment.start}
						className={cn("w-[3px] rounded-[1px]", segmentColors[segmentState(segment)])}
						title={`${timeFormat.format(segment.start)} - ${timeFormat.format(segment.start + 1_800_000)}: ${
							segment.total ? `${segment.ok}/${segment.total}` : "-"
						}`}
					/>
				))}
			</div>
			<span className="tabular-nums text-sm w-12 shrink-0">{formatUptimePct(uptime?.pct ?? null)}</span>
		</div>
	)
}

/**
 * Uptime history column. The map only feeds sorting (rebuilding the column makes the table re-sort);
 * cells render from the store. Sorts by 24h availability.
 */
export function getUptimeColumn(uptimeByMonitor: Map<string, MonitorUptime>): ColumnDef<NetworkMonitorRecord> {
	return {
		id: "uptime",
		meta: { label: t`Uptime` },
		accessorFn: (record) => uptimeByMonitor.get(record.id)?.pct ?? -1,
		header: ({ column }) => (
			<Button
				className={cn(
					"h-9 px-3 flex items-center gap-2 duration-50",
					column.getIsSorted() && "bg-accent/70 light:bg-accent text-accent-foreground/90"
				)}
				variant="ghost"
				onClick={() => column.toggleSorting(column.getIsSorted() === "asc")}
			>
				<ActivityIcon className="size-4" />
				{t`Uptime 24h`}
			</Button>
		),
		cell: ({ row }) => <UptimeCell monitor={row.original} />,
	}
}
