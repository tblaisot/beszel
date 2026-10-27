// bun has no DOM: stub localStorage, which store modules read at import time
const store = new Map<string, string>()

;(globalThis as { localStorage?: Storage }).localStorage ??= {
	get length() {
		return store.size
	},
	clear: () => store.clear(),
	getItem: (key: string) => store.get(key) ?? null,
	key: (index: number) => [...store.keys()][index] ?? null,
	removeItem: (key: string) => void store.delete(key),
	setItem: (key: string, value: string) => void store.set(key, String(value)),
}
