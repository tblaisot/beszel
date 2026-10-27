import { Trans } from "@lingui/react/macro"
import { getPagePath } from "@nanostores/router"
import { BellRingIcon } from "lucide-react"
import { buttonVariants } from "@/components/ui/button"
import { DropdownMenuItem } from "@/components/ui/dropdown-menu"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { $router, Link, navigate } from "@/components/router"
import { cn } from "@/lib/utils"
import { useAttention } from "./use-attention"

function useBadgeCount() {
	const data = useAttention()
	return data ? data.counts.critical + data.counts.warning : 0
}

/** Desktop nav icon for the Needs attention page, with the number of open problems. */
export function AttentionNavLink() {
	const count = useBadgeCount()
	return (
		<Tooltip>
			<TooltipTrigger asChild>
				<Link
					href={getPagePath($router, "attention")}
					className={cn("relative", buttonVariants({ variant: "ghost", size: "icon" }))}
					aria-label="Needs attention"
					onMouseEnter={() => import("@/fork/attention-page")}
				>
					<BellRingIcon className="h-[1.2rem] w-[1.2rem]" strokeWidth={1.5} />
					{count > 0 && (
						<span className="absolute top-1 end-1 min-w-4 h-4 px-1 rounded-full bg-red-500 text-white text-[10px] leading-4 text-center tabular-nums">
							{count > 99 ? "99+" : count}
						</span>
					)}
				</Link>
			</TooltipTrigger>
			<TooltipContent>
				<Trans>Needs attention</Trans>
			</TooltipContent>
		</Tooltip>
	)
}

/** Same entry for the mobile menu. */
export function AttentionMenuItem() {
	const count = useBadgeCount()
	return (
		<DropdownMenuItem onClick={() => navigate(getPagePath($router, "attention"))} className="flex items-center">
			<BellRingIcon className="h-4 w-4 me-2.5" strokeWidth={1.5} />
			<Trans>Needs attention</Trans>
			{count > 0 && <span className="ms-auto text-xs tabular-nums text-red-500">{count}</span>}
		</DropdownMenuItem>
	)
}
