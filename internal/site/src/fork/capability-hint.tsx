import type { ReactNode } from "react"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"

/**
 * Wraps disabled buttons of an agent that does not allow an action. A disabled button gets no pointer
 * events, so the tooltip hangs on the wrapper.
 */
export function CapabilityHint({ message, children }: { message: string; children: ReactNode }) {
	return (
		<Tooltip>
			<TooltipTrigger asChild>
				<span className="inline-flex cursor-not-allowed gap-2">{children}</span>
			</TooltipTrigger>
			<TooltipContent>{message}</TooltipContent>
		</Tooltip>
	)
}
