<script lang="ts">
	import type { Monitor } from '$lib/api';
	import { groupByService, groupMeta } from '$lib/grouping';
	import MonitorRow from './MonitorRow.svelte';

	let {
		monitors,
		showEnv,
		daysCaption,
		onCheck,
		onEdit,
		onDelete
	}: {
		monitors: Monitor[];
		showEnv: boolean;
		daysCaption: string;
		onCheck: (id: string) => void;
		onEdit: (monitor: Monitor) => void;
		onDelete: (id: string) => void;
	} = $props();

	const groups = $derived(groupByService(monitors));
</script>

<section class="groups">
	<div class="list-caption">{daysCaption}</div>
	{#each groups as group (group.service ?? '')}
		<section class="group">
			<div class="group-header">
				<h2 class:unassigned={group.service === null}>{group.service ?? 'Sin servicio'}</h2>
				<span class="group-meta">{groupMeta(group)}</span>
			</div>
			<div class="rows">
				{#each group.monitors as monitor (monitor.id)}
					<MonitorRow {monitor} {showEnv} {onCheck} {onEdit} {onDelete} />
				{/each}
			</div>
		</section>
	{/each}
</section>

<style>
	.groups {
		display: flex;
		flex-direction: column;
		gap: 24px;
	}

	.list-caption {
		text-align: right;
		font-size: 14px;
		color: var(--fg-2);
		margin-bottom: -12px;
	}

	.group {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.group-header {
		display: flex;
		align-items: baseline;
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
		font-size: 12px;
		color: var(--fg-2);
	}

	.rows {
		background: var(--m-surface);
		border: 1px solid var(--border-default);
		border-radius: 8px;
	}

	.rows > :global(* + *) {
		border-top: 1px solid var(--border-subtle);
	}
</style>
