<script lang="ts">
	import type { Monitor } from '$lib/api';
	import { groupByCompany, type CompanyIndex } from '$lib/companies';
	import { groupByService, groupMeta, type ServiceGroup } from '$lib/grouping';
	import type { Period } from '$lib/period';
	import AssignCompanyButton from './AssignCompanyButton.svelte';
	import Icon from './Icon.svelte';
	import MonitorRow from './MonitorRow.svelte';
	import PeriodSelect from './PeriodSelect.svelte';

	// monitors already carry the selected period's history and uptime.
	// twoColumns turns each service group into a card in a two-column grid
	// with stacked rows; scopeTitle (wide layout only) names what is shown.
	// companies (built from every monitor, not just these) puts the service
	// groups under company sections; while it is not enabled the list is
	// exactly the plain one.
	let {
		monitors,
		companies,
		showEnv,
		days,
		twoColumns,
		scopeTitle = null,
		onDaysChange,
		onAddToService,
		onAssignCompany,
		onCheck,
		onEdit,
		onDelete
	}: {
		monitors: Monitor[];
		companies: CompanyIndex;
		showEnv: boolean;
		days: Period;
		twoColumns: boolean;
		scopeTitle?: string | null;
		onDaysChange: (days: Period) => void;
		onAddToService: (service: string) => void;
		onAssignCompany: (service: string) => void;
		onCheck: (id: string) => void;
		onEdit: (monitor: Monitor) => void;
		onDelete: (id: string) => void;
	} = $props();

	const groups = $derived(groupByService(monitors));
	const layout = $derived(
		companies.enabled ? groupByCompany(groups, companies, (group) => group.monitors.length) : null
	);
</script>

{#snippet serviceGroups(list: ServiceGroup[])}
	<div class="group-grid" class:two-columns={twoColumns}>
		{#each list as group (group.service ?? '')}
			<section class="group" class:card={twoColumns}>
				<div class="group-header">
					<svelte:element
						this={layout ? 'h3' : 'h2'}
						class="group-title"
						class:unassigned={group.service === null}
					>
						{group.service ?? 'Sin servicio'}
					</svelte:element>
					<span class="group-meta">{groupMeta(group)}</span>
					{#if group.service !== null}
						{@const service = group.service}
						<AssignCompanyButton {service} onAssign={onAssignCompany} />
						<button
							type="button"
							class="add-button"
							aria-label="Agregar monitor a {service}"
							title="Agregar monitor a {service}"
							onclick={() => onAddToService(service)}
						>
							<Icon name="add-line" size={18} />
						</button>
					{/if}
				</div>
				<div class="rows">
					{#each group.monitors as monitor (monitor.id)}
						<MonitorRow {monitor} {showEnv} stacked={twoColumns} {onCheck} {onEdit} {onDelete} />
					{/each}
				</div>
			</section>
		{/each}
	</div>
{/snippet}

<section class="groups">
	<div class="list-toolbar" class:with-title={scopeTitle !== null}>
		{#if scopeTitle !== null}<span class="scope-title">{scopeTitle}</span>{/if}
		<PeriodSelect value={days} onChange={onDaysChange} />
	</div>
	{#if layout}
		<div class="company-list">
			{#each layout.sections as section (section.company ?? '')}
				<section class="company-section">
					<div class="company-header">
						<h2 class="company-title" class:unassigned={section.company === null}>
							{section.company ?? 'Sin empresa'}
						</h2>
						<span class="group-meta">{section.meta}</span>
					</div>
					{@render serviceGroups(section.items)}
				</section>
			{/each}
			{#if layout.noService.length > 0}
				{@render serviceGroups(layout.noService)}
			{/if}
		</div>
	{:else}
		{@render serviceGroups(groups)}
	{/if}
</section>

<style>
	.groups {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}

	.list-toolbar {
		display: flex;
		justify-content: flex-end;
		align-items: center;
		gap: 12px 16px;
		flex-wrap: wrap;
	}

	.list-toolbar.with-title {
		justify-content: space-between;
	}

	.scope-title {
		font-size: 14px;
		font-weight: 700;
	}

	.group-grid {
		display: flex;
		flex-direction: column;
		gap: 24px;
	}

	/* Two 600px cards side by side, the canvas's minimum card width. */
	.group-grid.two-columns {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(600px, 1fr));
		gap: 16px;
		align-items: start;
	}

	.group {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.group-header {
		display: flex;
		align-items: center;
		gap: 12px;
		flex-wrap: wrap;
	}

	.group-title,
	.company-title {
		margin: 0;
		font-size: 18px;
		font-weight: 700;
		line-height: 1.5;
	}

	.group-title.unassigned,
	.company-title.unassigned {
		color: var(--fg-2);
	}

	.company-list {
		display: flex;
		flex-direction: column;
		gap: 32px;
	}

	.company-section {
		display: flex;
		flex-direction: column;
		gap: 12px;
	}

	.company-header {
		display: flex;
		align-items: baseline;
		gap: 12px;
		flex-wrap: wrap;
	}

	.company-header .group-meta {
		flex: none;
	}

	.group-meta {
		flex: 1;
		font-size: 12px;
		color: var(--fg-2);
	}

	.add-button {
		width: 32px;
		height: 32px;
		display: flex;
		align-items: center;
		justify-content: center;
		flex-shrink: 0;
		padding: 0;
		border: 1px solid var(--border-default);
		border-radius: 4px;
		background: var(--m-surface);
		color: var(--m-ink);
		cursor: pointer;
	}

	.add-button:hover {
		background: var(--m-soft);
	}

	.rows {
		background: var(--m-surface);
		border: 1px solid var(--border-default);
		border-radius: 8px;
	}

	.rows > :global(* + *) {
		border-top: 1px solid var(--border-subtle);
	}

	/* No overflow: hidden here: the name tooltip and the actions menu
	   overflow the card on purpose. */
	.group.card {
		gap: 0;
		background: var(--m-surface);
		border: 1px solid var(--border-default);
		border-radius: 8px;
	}

	.card .group-header {
		padding: 12px 12px 12px 16px;
	}

	.card .group-title {
		font-size: 16px;
	}

	.card .rows {
		border: 0;
		border-top: 1px solid var(--border-subtle);
		border-radius: 0;
		background: none;
	}
</style>
