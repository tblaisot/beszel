import { t } from "@lingui/core/macro"
import LineChartDefault from "@/components/charts/line-chart"
import { ChartCard } from "@/components/routes/system/chart-card"
import { decimalString } from "@/lib/utils"
import type { ChartData } from "@/types"

function formatFreq(mhz: number, digits = 2) {
	return mhz >= 1000 ? `${decimalString(mhz / 1000, digits)} GHz` : `${Math.round(mhz)} MHz`
}

/** CPU frequency over time. Only rendered when the agent reports cpufreq (Linux hosts that expose it). */
export function CpuFreqChart({
	chartData,
	grid,
	dataEmpty,
}: {
	chartData: ChartData
	grid: boolean
	dataEmpty: boolean
}) {
	if (!chartData.systemStats.some((record) => record.stats?.cf)) {
		return null
	}
	return (
		<ChartCard
			empty={dataEmpty}
			grid={grid}
			title={t`CPU Frequency`}
			description={t`Average and highest core frequency`}
			legend={true}
		>
			<LineChartDefault
				chartData={chartData}
				domain={[0, "auto"]}
				legend={true}
				tickFormatter={(value) => formatFreq(value, 1)}
				contentFormatter={(item) => formatFreq(item.value)}
				dataPoints={[
					{
						label: t`Average`,
						color: "hsl(217, 91%, 60%)",
						dataKey: ({ stats }) => stats?.cf?.[0],
					},
					{
						label: t`Highest core`,
						color: "hsl(25, 95%, 53%)",
						dataKey: ({ stats }) => stats?.cf?.[1],
					},
				]}
			></LineChartDefault>
		</ChartCard>
	)
}
