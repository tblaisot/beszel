import { expect, test } from "bun:test"
import { type CapacityMap, hostCapacity, usageValue } from "./usage"

const GIB = 1024 ** 3
const caps: CapacityMap = {
	nas: { cores: 8, memory: 32 * GIB },
	mosquitto: { cores: 8, memory: 0.375 * GIB, parent: "nas" },
	alone: { cores: 4, memory: 4 * GIB },
}

test("machine mode keeps the reported figure", () => {
	expect(usageValue("sys-cpu", "machine", 12.5, "nas", caps)).toEqual({ value: 12.5, unit: "pct" })
	expect(usageValue("ct-mem", "machine", 100, "nas", caps)).toEqual({ value: 100 * 1024 * 1024, unit: "bytes" })
})

test("absolute CPU is cores in use", () => {
	expect(usageValue("sys-cpu", "absolute", 50, "alone", caps)).toEqual({ value: 2, unit: "cores" })
	expect(usageValue("ct-cpu", "absolute", 10, "nas", caps)).toEqual({ value: 0.8, unit: "cores" })
})

test("absolute memory of a system follows its own total", () => {
	// 66% of a 384 MiB guest is about 253 MiB, not a third of the host
	const v = usageValue("sys-mem", "absolute", 66, "mosquitto", caps)
	expect(v?.unit).toBe("bytes")
	expect(v?.value).toBeCloseTo(0.66 * 0.375 * GIB)
})

test("host mode divides by the linked parent", () => {
	const v = usageValue("sys-mem", "host", 66, "mosquitto", caps)
	expect(v?.unit).toBe("pct")
	expect(v?.value).toBeCloseTo((0.66 * 0.375 * 100) / 32)
	// a system without a parent is measured against itself
	expect(usageValue("sys-cpu", "host", 40, "alone", caps)).toEqual({ value: 40, unit: "pct" })
})

test("container memory in host mode", () => {
	const v = usageValue("ct-mem", "host", 512, "mosquitto", caps)
	expect(v?.value).toBeCloseTo((512 * 1024 * 1024 * 100) / (32 * GIB))
})

test("unknown capacity gives undefined outside machine mode", () => {
	expect(usageValue("sys-cpu", "absolute", 10, "ghost", caps)).toBeUndefined()
	expect(usageValue("ct-mem", "host", 10, "ghost", caps)).toBeUndefined()
	expect(usageValue("sys-cpu", "machine", 10, "ghost", caps)).toEqual({ value: 10, unit: "pct" })
})

test("hostCapacity falls back when the parent is unknown", () => {
	expect(hostCapacity({ a: { cores: 2, memory: 1, parent: "missing" } }, "a")?.cores).toBe(2)
})
