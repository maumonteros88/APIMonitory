<script lang="ts">
	import type { Monitor } from '$lib/api';
	import { groupByService, groupMeta } from '$lib/grouping';
	import type { Period } from '$lib/period';
	import Icon from './Icon.svelte';
	import MonitorRow from './MonitorRow.svelte';
	import PeriodSelect from './PeriodSelect.svelte';

	// monitors already carry the selected period's history and uptime.
	// twoColumns turns each service group into a card in a two-column grid
	// with stacked rows; scopeTitle (wide layout only) names what is shown.
	let {
		monitors,
		showEnv,
		days,
		twoColumns,
		scopeTitle = null,
		onDaysChange,
		onAddToService,
		onCheck,
		onEdit,
		onDelete
	}: {
		monitors: Monitor[];
		showEnv: boolean;
		days: Period;
		twoColumns: boolean;
		scopeTitle?: string | null;
		onDaysChange: (days: Period) => void;
		onAddToService: (service: string) => void;
		onCheck: (id: string) => void;
		onEdit: (monitor: Monitor) => void;
		onDelete: (id: string) => void;
	} = $props();

	const groups = $derived(groupByService(monitors));
</script>

<section class="groups">
	<div class="list-toolbar" class:with-title={scopeTitle !== null}>
		{#if scopeTitle !== null}<span class="scope-title">{scopeTitle}</span>{/if}
		<PeriodSelect value={days} onChange={onDaysChange} />
	</div>
	<div class="group-grid" class:two-columns={twoColumns}>
		{#each groups as group (group.service ?? '')}
			<section class="group" class:card={twoColumns}>
				<div class="group-header">
					<h2 class:unassigned={group.service === null}>{group.service ?? 'Sin servicio'}</h2>
					<span class="group-meta">{groupMeta(group)}</span>
					{#if group.service !== null}
						{@const service = group.service}
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

	.group-header h2 {
		margin: 0;
		font-size: 18px;
		font-weight: 700;
		line-height: 1.5;
	}

	.group-header h2.unassigned {
		color: var(--fg-2);
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

	.card .group-header h2 {
		font-size: 16px;
	}

	.card .rows {
		border: 0;
		border-top: 1px solid var(--border-subtle);
		border-radius: 0;
		background: none;
	}
</style>
