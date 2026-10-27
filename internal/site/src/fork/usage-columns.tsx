import type { CellContext, ColumnDef, Row, SortingState } from "@tanstack/react-table"
import { useEffect, useRef } from "react"
import { flexRender } from "@tanstack/react-table"
import { useLingui } from "@lingui/react/macro"
import { Trans } from "@lingui/react/macro"
import { useStore } from "@nanostores/react"
import { GaugeIcon } from "lucide-react"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { MeterState, SystemStatus } from "@/lib/enums"
import { pb } from "@/lib/api"
import { $systems, $userSettings } from "@/lib/stores"
import { cn, decimalString, formatBytes } from "@/lib/utils"
import type { ContainerRecord, SystemRecord } from "@/types"
import { $capacity, $usageMode, loadCapacity, useUsage } from "./use-usage"
import { type CapacityMap, USAGE_MODES, type UsageKind, type UsageMode, type UsageValue, usageValue } from "./usage"

export function formatUsage({ value, unit }: UsageValue) {
	if (unit === "pct") return `${decimalString(value, value >= 10 ? 1 : 2)}%`
	if (unit === "cores") return `${decimalString(value, value >= 10 ? 1 : value >= 1 ? 2 : 3)} cores`
	const bytes = formatBytes(value, false, undefined, false)
	return `${decimalString(bytes.value, bytes.value >= 10 ? 1 : 2)} ${bytes.unit}`
}

/** Mode picker for the table toolbars. */
export function UsageModeToggle({ className }: { className?: string }) {
	const { t } = useLingui()
	const mode = useStore($usageMode)
	const labels: Record<UsageMode, string> = {
		machine: t`Default`,
		absolute: t`Absolute`,
		host: t`% of host`,
	}
	return (
		<Select value={mode} onValueChange={(value) => $usageMode.set(value as UsageMode)}>
			<SelectTrigger className={cn("w-auto gap-2", className)} aria-label={t`CPU and memory units`}>
				<GaugeIcon className="size-4 shrink-0 text-muted-foreground" />
				<SelectValue />
			</SelectTrigger>
			<SelectContent>
				{USAGE_MODES.map((value) => (
					<SelectItem key={value} value={value}>
						{labels[value]}
					</SelectItem>
				))}
			</SelectContent>
		</Select>
	)
}

const STATUS_COLORS = {
	up: "bg-green-500",
	pending: "bg-yellow-500",
	down: "bg-red-500",
	paused: "bg-primary/40",
}

function meterState(value: number, warn: number, crit: number): MeterState {
	return value >= crit ? MeterState.Crit : value >= warn ? MeterState.Warn : MeterState.Good
}

type SystemCell = CellContext<SystemRecord, unknown>

/** Systems table cell: same meter as the default one, with the value in the chosen mode. */
function UsageMeterCell({
	info,
	kind,
	original,
}: {
	info: SystemCell
	kind: UsageKind
	original: ColumnDef<SystemRecord>["cell"]
}) {
	const mode = useStore($usageMode)
	const { map: caps } = useStore($capacity)
	const { colorWarn = 65, colorCrit = 90 } = useStore($userSettings, { keys: ["colorWarn", "colorCrit"] })
	const system = info.row.original
	const raw = (kind === "sys-cpu" ? system.info.cpu : system.info.mp) || 0
	const usage = mode === "machine" ? undefined : usageValue(kind, mode, raw, system.id, caps)
	if (!usage) return flexRender(original, info)

	// the bar is always a percentage: the host share in host mode, the system's own share otherwise
	const bar = usage.unit === "pct" ? usage.value : raw
	const state = meterState(bar, colorWarn, colorCrit)
	const color =
		(system.status !== SystemStatus.Up && STATUS_COLORS.paused) ||
		(state === MeterState.Good && STATUS_COLORS.up) ||
		(state === MeterState.Warn && STATUS_COLORS.pending) ||
		STATUS_COLORS.down
	return (
		<div className="flex gap-2 items-center tabular-nums tracking-tight w-full min-w-0">
			<span className="min-w-8 shrink-0">{formatUsage(usage)}</span>
			<span className="flex-1 min-w-8 grid bg-muted h-[1em] rounded-sm overflow-hidden">
				<span className={cn("h-full", color)} style={{ width: `${Math.min(100, bar)}%` }}></span>
			</span>
		</div>
	)
}

/** The figure a row is sorted by in the chosen mode. */
function sortValue(kind: UsageKind, record: SystemRecord | ContainerRecord, mode: UsageMode, caps: CapacityMap) {
	let raw: number
	let systemId: string
	if ("info" in record) {
		raw = (kind === "sys-cpu" ? record.info.cpu : record.info.mp) || 0
		systemId = record.id
	} else {
		raw = kind === "ct-cpu" ? record.cpu : record.memory
		systemId = record.system
	}
	return usageValue(kind, mode, raw, systemId, caps)?.value ?? raw
}

/**
 * Table rows are memoized, so rebuilt column definitions never reach rows that are already
 * rendered. The cells therefore always wrap the originals and read the mode from the store;
 * the column definitions only need rebuilding so that sorting follows the displayed value (cached
 * accessor values are left alone; a custom sortingFn does the work, and the table re-sorts via
 * useResortOnChange).
 */

/** Replaces the CPU and Memory columns of the systems table. */
export function withSystemUsage(columns: ColumnDef<SystemRecord>[], mode: UsageMode, caps: CapacityMap) {
	return columns.map((column): ColumnDef<SystemRecord> => {
		const kind: UsageKind | undefined = column.id === "cpu" ? "sys-cpu" : column.id === "memory" ? "sys-mem" : undefined
		if (!kind) return column
		return {
			...column,
			...(mode !== "machine" && {
				sortingFn: (a: Row<SystemRecord>, b: Row<SystemRecord>) =>
					sortValue(kind, a.original, mode, caps) - sortValue(kind, b.original, mode, caps),
			}),
			cell: (info) => <UsageMeterCell info={info} kind={kind} original={column.cell} />,
		} as ColumnDef<SystemRecord>
	})
}

type ContainerCell = CellContext<ContainerRecord, unknown>

function UsageTextCell({
	info,
	kind,
	original,
}: {
	info: ContainerCell
	kind: UsageKind
	original: ColumnDef<ContainerRecord>["cell"]
}) {
	const mode = useStore($usageMode)
	const { map: caps } = useStore($capacity)
	const record = info.row.original
	const usage =
		mode === "machine"
			? undefined
			: usageValue(kind, mode, kind === "ct-cpu" ? record.cpu : record.memory, record.system, caps)
	if (!usage) return flexRender(original, info)
	return <span className="ms-1 tabular-nums">{formatUsage(usage)}</span>
}

/** Replaces the CPU and Memory columns of the containers table. */
export function withContainerUsage(columns: ColumnDef<ContainerRecord>[], mode: UsageMode, caps: CapacityMap) {
	return columns.map((column): ColumnDef<ContainerRecord> => {
		const kind: UsageKind | undefined = column.id === "cpu" ? "ct-cpu" : column.id === "memory" ? "ct-mem" : undefined
		if (!kind) return column
		return {
			...column,
			...(mode !== "machine" && {
				sortingFn: (a: Row<ContainerRecord>, b: Row<ContainerRecord>) =>
					sortValue(kind, a.original, mode, caps) - sortValue(kind, b.original, mode, caps),
			}),
			cell: (info) => <UsageTextCell info={info} kind={kind} original={column.cell} />,
		} as ColumnDef<ContainerRecord>
	})
}

/** "Parent host" row for the system edit dialog (two grid cells). Saves immediately on change. */
export function ParentHostField({ system }: { system?: SystemRecord }) {
	const { t } = useLingui()
	const systems = useStore($systems)
	const { map: caps } = useStore($capacity)
	useUsage()
	if (!system) return null
	const current = caps[system.id]?.parent ?? "none"
	return (
		<>
			<Label htmlFor="parent-host" className="xs:text-end whitespace-pre">
				<Trans>Parent host</Trans>
			</Label>
			<Select
				value={current}
				onValueChange={async (value) => {
					await pb.collection("systems").update(system.id, { parent: value === "none" ? "" : value })
					await loadCapacity(true)
				}}
			>
				<SelectTrigger id="parent-host" aria-label={t`Parent host`}>
					<SelectValue />
				</SelectTrigger>
				<SelectContent>
					<SelectItem value="none">{t`None`}</SelectItem>
					{systems
						.filter((other) => other.id !== system.id)
						.map((other) => (
							<SelectItem key={other.id} value={other.id}>
								{other.name}
							</SelectItem>
						))}
				</SelectContent>
			</Select>
		</>
	)
}

/** Re-sorts a table when the mode or capacities change (sorted rows are cached on the sorting state). */
export function useResortOnChange(setSorting: (updater: (sorting: SortingState) => SortingState) => void) {
	const { mode, caps } = useUsage()
	const first = useRef(true)
	useEffect(() => {
		if (first.current) {
			first.current = false
			return
		}
		setSorting((sorting) => [...sorting])
	}, [mode, caps])
}
