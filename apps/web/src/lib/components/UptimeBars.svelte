<script lang="ts">
	import type { DayHistory } from '$lib/api';
	import { dayColor } from '$lib/format';
	import DayTooltip from './DayTooltip.svelte';

	let {
		history,
		monitorId,
		height = 34
	}: { history: DayHistory[]; monitorId: string; height?: number } = $props();

	let hoveredDate = $state<string | null>(null);
	let tooltipPos = $state({ x: 0, y: 0 });

	const hoveredDay = $derived(history.find((d) => d.date === hoveredDate) ?? null);

	function showTooltip(day: DayHistory, event: MouseEvent) {
		hoveredDate = day.date;
		tooltipPos = { x: event.clientX, y: event.clientY };
	}

	function moveTooltip(event: MouseEvent) {
		tooltipPos = { x: event.clientX, y: event.clientY };
	}

	function hideTooltip() {
		hoveredDate = null;
	}
</script>

<div class="bars" style="height: {height}px" onmouseleave={hideTooltip} role="img" aria-label="Disponibilidad diaria de los últimos {history.length} días">
	{#each history as day (day.date)}
		<div
			class="bar"
			role="presentation"
			style="background: {hoveredDate === day.date ? 'var(--m-none)' : dayColor(day.state)}"
			onmouseenter={(event) => showTooltip(day, event)}
			onmousemove={moveTooltip}
		></div>
	{/each}
</div>

{#if hoveredDay}
	<DayTooltip day={hoveredDay} x={tooltipPos.x} y={tooltipPos.y} {monitorId} />
{/if}

<style>
	.bars {
		display: flex;
		gap: 2px;
	}

	.bar {
		flex: 1;
		border-radius: 2px;
		cursor: pointer;
		transition: background-color 120ms ease;
	}
</style>
