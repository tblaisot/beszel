import { useLingui } from "@lingui/react/macro"
import { useStore } from "@nanostores/react"
import { useEffect, useState } from "react"
import { Badge } from "@/components/ui/badge"
import { Card, CardTitle } from "@/components/ui/card"
import { isAdmin, pb } from "@/lib/api"
import { $auditVersion } from "./control-buttons"

interface AuditRecord {
	id: string
	created: string
	user_email: string
	system_name: string
	kind: string
	target: string
	action: string
	result: "ok" | "error" | "refused" | "started" | "ended"
	message: string
}

const REFRESH_MS = 60_000

/** The latest actions taken on systems, for admins. */
export function AuditList() {
	const { t } = useLingui()
	const version = useStore($auditVersion)
	const [records, setRecords] = useState<AuditRecord[]>([])

	// biome-ignore lint/correctness/useExhaustiveDependencies: version only triggers a refetch
	useEffect(() => {
		if (!isAdmin()) return
		let cancelled = false
		const load = () => {
			if (document.hidden) return
			pb.collection<AuditRecord>("fork_audit")
				.getList(1, 15, { sort: "-created", requestKey: null })
				.then((res) => !cancelled && setRecords(res.items))
				.catch((error) => console.error("Failed to fetch audit log:", error))
		}
		load()
		const timer = setInterval(load, REFRESH_MS)
		return () => {
			cancelled = true
			clearInterval(timer)
		}
	}, [version])

	if (!isAdmin() || !records.length) {
		return null
	}
	return (
		<Card className="px-4 py-4 sm:px-6">
			<CardTitle className="text-sm uppercase tracking-wide text-muted-foreground mb-3">{t`Recent actions`}</CardTitle>
			<ul className="divide-y">
				{records.map((record) => (
					<li key={record.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2 text-sm">
						<span className="text-xs text-muted-foreground tabular-nums">
							{new Date(record.created.replace(" ", "T")).toLocaleString()}
						</span>
						<span className="font-medium">
							{record.action} {record.target}
						</span>
						<span className="text-muted-foreground">
							{record.system_name} · {record.user_email}
						</span>
						<Badge
							variant={record.result === "error" || record.result === "refused" ? "destructive" : "outline"}
							className="text-[10px]"
						>
							{record.result}
						</Badge>
						{record.message && record.result !== "ok" && (
							<span className="text-xs text-muted-foreground truncate">{record.message}</span>
						)}
					</li>
				))}
			</ul>
		</Card>
	)
}
