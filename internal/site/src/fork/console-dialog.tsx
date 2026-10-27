import { useLingui } from "@lingui/react/macro"
import { FitAddon } from "@xterm/addon-fit"
import { Terminal } from "@xterm/xterm"
import "@xterm/xterm/css/xterm.css"
import { useEffect, useState } from "react"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { pb } from "@/lib/api"

type Status = "connecting" | "open" | "ended" | "error"

/** WebSocket URL of the hub's console endpoint, following the page's scheme and any base path. */
function consoleUrl(ticket: string, cols: number, rows: number) {
	const url = new URL("api/beszel/fork/exec", new URL(pb.baseURL, window.location.href))
	url.protocol = url.protocol === "https:" ? "wss:" : "ws:"
	url.search = new URLSearchParams({ ticket, cols: String(cols), rows: String(rows) }).toString()
	return url.toString()
}

export default function ConsoleDialog({
	systemId,
	containerId,
	label,
	onClose,
}: {
	systemId: string
	containerId: string
	label: string
	onClose: () => void
}) {
	const { t } = useLingui()
	// the dialog content mounts after this component, so the element arrives through state
	const [element, setElement] = useState<HTMLDivElement | null>(null)
	const [status, setStatus] = useState<Status>("connecting")
	const [message, setMessage] = useState("")

	useEffect(() => {
		if (!element) return
		let disposed = false
		let socket: WebSocket | undefined

		const terminal = new Terminal({
			cursorBlink: true,
			fontSize: 13,
			fontFamily: "ui-monospace, SFMono-Regular, Menlo, Consolas, monospace",
			theme: { background: "#0b0b0c" },
			scrollback: 5000,
		})
		const fit = new FitAddon()
		terminal.loadAddon(fit)
		terminal.open(element)
		fit.fit()
		const encoder = new TextEncoder()

		const onResize = () => {
			if (!disposed) fit.fit()
		}
		const observer = new ResizeObserver(onResize)
		observer.observe(element)

		;(async () => {
			try {
				const { ticket } = await pb.send<{ ticket: string }>("/api/beszel/fork/exec-ticket", {
					method: "POST",
					body: { system: systemId, container: containerId },
				})
				if (disposed) return
				socket = new WebSocket(consoleUrl(ticket, terminal.cols, terminal.rows))
				socket.binaryType = "arraybuffer"
				socket.onopen = () => {
					setStatus("open")
					terminal.focus()
				}
				socket.onmessage = (event) => {
					if (typeof event.data === "string") {
						const control = JSON.parse(event.data) as { type: string; code?: number; message?: string }
						if (control.type === "exit") {
							setStatus("ended")
							setMessage(t`The shell exited with code ${control.code}`)
						} else if (control.type === "error") {
							setStatus("error")
							setMessage(control.message ?? "")
						}
						return
					}
					terminal.write(new Uint8Array(event.data as ArrayBuffer))
				}
				socket.onclose = () => setStatus((current) => (current === "open" ? "ended" : current))
				socket.onerror = () => setStatus((current) => (current === "open" ? current : "error"))
				terminal.onData((data) => socket?.readyState === WebSocket.OPEN && socket.send(encoder.encode(data)))
				terminal.onResize(({ cols, rows }) => {
					if (socket?.readyState === WebSocket.OPEN) socket.send(JSON.stringify({ type: "resize", cols, rows }))
				})
			} catch (error: unknown) {
				const response = (error as { response?: { message?: string } }).response
				setStatus("error")
				setMessage(response?.message ?? (error as Error).message)
			}
		})()

		return () => {
			disposed = true
			observer.disconnect()
			socket?.close()
			terminal.dispose()
		}
	}, [element, systemId, containerId, t])

	return (
		<Dialog open onOpenChange={(open) => !open && onClose()}>
			<DialogContent className="max-w-5xl w-[95vw]">
				<DialogHeader>
					<DialogTitle>
						{t`Console`}: {label}
					</DialogTitle>
					<DialogDescription>
						{status === "connecting" && t`Connecting...`}
						{status === "open" &&
							t`Connected. Closing this window ends the shell. The session is recorded in the audit log.`}
						{(status === "ended" || status === "error") && message}
						{status === "ended" && !message && t`Session ended`}
					</DialogDescription>
				</DialogHeader>
				<div ref={setElement} className="h-[26rem] w-full rounded-md bg-[#0b0b0c] p-2 overflow-hidden" />
			</DialogContent>
		</Dialog>
	)
}
