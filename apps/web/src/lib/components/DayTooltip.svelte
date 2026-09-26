<script lang="ts">
	import type { DayHistory } from '$lib/api';
	import { fmtDate, fmtMins } from '$lib/format';
	import Icon from './Icon.svelte';

	let { day, x, y }: { day: DayHistory; x: number; y: number } = $props();

	const note = $derived.by(() => {
		if (day.state === 'none') return 'Sin datos registrados para este día.';
		if (day.checks > 0) return `${day.checks} verificaciones, ${day.fails} fallidas.`;
		return 'Sin caídas registradas este día.';
	});
</script>

<div class="tooltip" style="left: {x}px; top: {y}px;">
	<span class="tooltip-date">{fmtDate(day.date)}</span>

	{#if day.downMinutes > 0}
		<div class="tooltip-row tooltip-row-bad">
			<Icon name="close-circle-fill" size={18} />
			<span class="tooltip-row-label">Caída total</span>
			<span class="tooltip-row-value">{fmtMins(day.downMinutes)}</span>
		</div>
	{/if}

	{#if day.degradedMinutes > 0}
		<div class="tooltip-row tooltip-row-warn">
			<Icon name="error-warning-fill" size={18} />
			<span class="tooltip-row-label">Degradación</span>
			<span class="tooltip-row-value">{fmtMins(day.degradedMinutes)}</span>
		</div>
	{/if}

	<span class="tooltip-note">{note}</span>
</div>

<style>
	.tooltip {
		position: fixed;
		width: 320px;
		padding: 20px;
		background: var(--m-surface);
		border: 1px solid var(--border-default);
		border-radius: 8px;
		box-shadow: 0 12px 28px -6px var(--m-shadow);
		display: flex;
		flex-direction: column;
		gap: 12px;
		z-index: 50;
		pointer-events: none;
	}

	.tooltip-date {
		font-weight: 700;
		color: var(--m-ink);
	}

	.tooltip-row {
		display: flex;
		align-items: center;
		gap: 10px;
		background: var(--m-soft);
		border-radius: 4px;
		padding: 10px 12px;
	}

	.tooltip-row :global(svg) {
		flex-shrink: 0;
	}

	.tooltip-row-bad :global(svg) {
		color: var(--m-bad-tx);
	}

	.tooltip-row-warn :global(svg) {
		color: #e0a800;
	}

	.tooltip-row-label {
		flex: 1;
		font-size: 14px;
		color: var(--m-ink);
	}

	.tooltip-row-value {
		font-size: 12px;
		color: var(--fg-2);
		font-weight: 600;
	}

	.tooltip-note {
		font-size: 12px;
		color: var(--fg-2);
	}
</style>
