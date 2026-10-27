import { useLingui } from "@lingui/react/macro"
import { useStore } from "@nanostores/react"
import { atom } from "nanostores"
import { PlayIcon, RotateCwIcon, SquareIcon } from "lucide-react"
import { useState } from "react"
import {
	AlertDialog,
	AlertDialogAction,
	AlertDialogCancel,
	AlertDialogContent,
	AlertDialogDescription,
	AlertDialogFooter,
	AlertDialogHeader,
	AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { Button } from "@/components/ui/button"
import { toast } from "@/components/ui/use-toast"
import { isAdmin, pb } from "@/lib/api"
import { $allSystemsById } from "@/lib/stores"

export type ControlKind = "container" | "service"
type ControlAction = "start" | "stop" | "restart"

/** Bumped after every action so the audit list refetches. */
export const $auditVersion = atom(0)

const ICONS = { start: PlayIcon, stop: SquareIcon, restart: RotateCwIcon }

/**
 * Start, stop and restart for one container or systemd service. Only shown to admins, and only
 * for systems whose agent reports the `control` action (FORK_ACTIONS=control). The hub checks
 * all of that again.
 */
export function ControlButtons({
	systemId,
	kind,
	target,
	label,
}: {
	systemId?: string
	kind: ControlKind
	target: string
	label: string
}) {
	const { t } = useLingui()
	const system = useStore($allSystemsById)[systemId ?? ""]
	const [pending, setPending] = useState<ControlAction | null>(null)
	const [busy, setBusy] = useState(false)

	if (!isAdmin() || !systemId || !system?.info?.fa?.includes("control")) {
		return null
	}

	const names: Record<ControlAction, string> = { start: t`Start`, stop: t`Stop`, restart: t`Restart` }

	async function run(action: ControlAction) {
		setBusy(true)
		try {
			const res = await pb.send<{ result: string }>("/api/beszel/fork/actions", {
				method: "POST",
				body: { system: systemId, kind, target, action },
			})
			toast({ title: `${names[action]}: ${label}`, description: res.result })
		} catch (error: unknown) {
			const response = (error as { response?: { error?: string; message?: string } }).response
			toast({
				title: t`Error`,
				description: response?.error ?? response?.message ?? (error as Error).message,
				variant: "destructive",
			})
		} finally {
			setBusy(false)
			setPending(null)
			$auditVersion.set($auditVersion.get() + 1)
		}
	}

	return (
		<>
			<div className="flex gap-2">
				{(["start", "stop", "restart"] as ControlAction[]).map((action) => {
					const Icon = ICONS[action]
					return (
						<Button key={action} size="sm" variant="outline" disabled={busy} onClick={() => setPending(action)}>
							<Icon className="size-3.5 me-1.5" />
							{names[action]}
						</Button>
					)
				})}
			</div>
			<AlertDialog open={pending !== null} onOpenChange={(open) => !open && !busy && setPending(null)}>
				<AlertDialogContent>
					<AlertDialogHeader>
						<AlertDialogTitle>
							{pending ? names[pending] : ""} {label}?
						</AlertDialogTitle>
						<AlertDialogDescription>
							{pending === "stop"
								? t`The ${kind} will stop until it is started again.`
								: t`This acts on the host straight away.`}{" "}
							{t`The action is recorded in the audit log.`}
						</AlertDialogDescription>
					</AlertDialogHeader>
					<AlertDialogFooter>
						<AlertDialogCancel disabled={busy}>{t`Cancel`}</AlertDialogCancel>
						<AlertDialogAction
							disabled={busy}
							onClick={(event) => {
								event.preventDefault()
								if (pending) run(pending)
							}}
						>
							{pending ? names[pending] : ""}
						</AlertDialogAction>
					</AlertDialogFooter>
				</AlertDialogContent>
			</AlertDialog>
		</>
	)
}
