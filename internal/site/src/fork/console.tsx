import { useLingui } from "@lingui/react/macro"
import { useStore } from "@nanostores/react"
import { TerminalIcon } from "lucide-react"
import { lazy, Suspense, useState } from "react"
import { Button } from "@/components/ui/button"
import { isAdmin } from "@/lib/api"
import { $allSystemsById } from "@/lib/stores"

const ConsoleDialog = lazy(() => import("./console-dialog"))

/**
 * Opens a shell in a container. Admins only, and only for systems whose agent reports the `exec`
 * action (FORK_ACTIONS=exec). The terminal (xterm.js) is loaded when the button is first used.
 */
export function ConsoleButton({
	systemId,
	containerId,
	label,
}: {
	systemId?: string
	containerId: string
	label: string
}) {
	const { t } = useLingui()
	const system = useStore($allSystemsById)[systemId ?? ""]
	const [open, setOpen] = useState(false)

	if (!isAdmin() || !systemId || !system?.info?.fa?.includes("exec")) {
		return null
	}
	return (
		<>
			<Button size="sm" variant="outline" onClick={() => setOpen(true)}>
				<TerminalIcon className="size-3.5 me-1.5" />
				{t`Console`}
			</Button>
			{open && (
				<Suspense fallback={null}>
					<ConsoleDialog systemId={systemId} containerId={containerId} label={label} onClose={() => setOpen(false)} />
				</Suspense>
			)}
		</>
	)
}
