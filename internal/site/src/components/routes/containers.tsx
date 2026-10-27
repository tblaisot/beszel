import { useLingui } from "@lingui/react/macro"
import { memo, useEffect, useMemo } from "react"
import ContainersTable from "@/components/containers-table/containers-table"
import { FooterRepoLink } from "@/components/footer-repo-link"
import { TopConsumers } from "@/fork/top-consumers"

export default memo(() => {
	const { t } = useLingui()

	useEffect(() => {
		document.title = `${t`All Containers`} / Beszel`
	}, [t])

	return useMemo(
		() => (
			<>
				<TopConsumers />
				<ContainersTable />
				<FooterRepoLink />
			</>
		),
		[]
	)
})
