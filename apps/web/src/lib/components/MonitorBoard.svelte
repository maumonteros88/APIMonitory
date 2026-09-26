<script lang="ts">
	import type { Monitor } from '$lib/api';
	import { ago, statusMeta, tileStyle, uptimeShort } from '$lib/format';

	let {
		monitors,
		daysCaption,
		onCheck
	}: { monitors: Monitor[]; daysCaption: string; onCheck: (id: string) => void } = $props();

	const tiles = $derived(
		monitors.map((m) => {
			const style = tileStyle(m.status, m.uptimePercent);
			const big = monitors.length > 4 && m.status === 'down';
			return {
				id: m.id,
				name: m.name,
				statusLabel: statusMeta(m.status).label,
				latency: m.lastCheck ? `${m.lastCheck.latencyMs} ms` : '',
				uptimeShort: uptimeShort(m.uptimePercent),
				lastAgo: m.checking ? 'Verificando…' : m.lastCheck ? `Verificada ${ago(m.lastCheck.at)}` : 'Sin verificar',
				bg: style.bg,
				fg: style.fg,
				flexBasis: big ? '2 1 440px' : '1 1 220px',
				minHeight: big ? '240px' : '170px',
				nameSize: big ? '28px' : '21px',
				upSize: big ? '21px' : '16px'
			};
		})
	);
</script>

<section class="board">
	<div class="board-header">
		<div class="legend">
			<span class="legend-item"><span class="legend-dot" style="background: #289B4F"></span>Operativa</span>
			<span class="legend-item"><span class="legend-dot" style="background: #FFC700"></span>Degradada</span>
			<span class="legend-item"><span class="legend-dot" style="background: #DF4730"></span>Sin respuesta</span>
		</div>
		<span class="days-caption">{daysCaption}</span>
	</div>

	{#if tiles.length === 0}
		<div class="board-empty">No hay APIs para monitorear.</div>
	{:else}
		<div class="tiles">
			{#each tiles as tile (tile.id)}
				<button
					type="button"
					class="tile"
					title="Verificar ahora"
					style="flex: {tile.flexBasis}; min-height: {tile.minHeight}; background: {tile.bg}; color: {tile.fg}"
					onclick={() => onCheck(tile.id)}
				>
					<div class="tile-top">
						<span class="tile-status">{tile.statusLabel}</span>
						<span class="tile-latency">{tile.latency}</span>
					</div>
					<div class="tile-mid">
						<span class="tile-name" style="font-size: {tile.nameSize}">{tile.name}</span>
						<span class="tile-uptime" style="font-size: {tile.upSize}">{tile.uptimeShort}</span>
					</div>
					<span class="tile-ago">{tile.lastAgo}</span>
				</button>
			{/each}
		</div>
	{/if}
</section>

<style>
	.board {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.board-header {
		display: flex;
		justify-content: space-between;
		gap: 16px;
		flex-wrap: wrap;
		font-size: 12px;
		color: var(--fg-2);
	}

	.legend {
		display: flex;
		gap: 16px;
		flex-wrap: wrap;
	}

	.legend-item {
		display: flex;
		align-items: center;
		gap: 6px;
	}

	.legend-dot {
		width: 10px;
		height: 10px;
		border-radius: 2px;
	}

	.days-caption {
		font-size: 14px;
	}

	.board-empty {
		padding: 48px 24px;
		text-align: center;
		font-size: 14px;
		color: var(--fg-2);
		background: var(--m-surface);
		border: 1px solid var(--border-default);
		border-radius: 8px;
	}

	.tiles {
		display: flex;
		flex-wrap: wrap;
		gap: 4px;
	}

	.tile {
		box-sizing: border-box;
		border: 0;
		border-radius: 4px;
		padding: 16px;
		display: flex;
		flex-direction: column;
		justify-content: space-between;
		align-items: flex-start;
		gap: 12px;
		text-align: left;
		cursor: pointer;
		min-width: 0;
		font-family: inherit;
		transition: filter 150ms ease-out;
	}

	.tile:hover {
		filter: brightness(1.08);
	}

	.tile-top {
		display: flex;
		justify-content: space-between;
		align-items: flex-start;
		gap: 8px;
		width: 100%;
	}

	.tile-status {
		font-size: 12px;
		font-weight: 600;
		opacity: 0.9;
	}

	.tile-latency {
		font-size: 12px;
		opacity: 0.9;
		white-space: nowrap;
	}

	.tile-mid {
		display: flex;
		flex-direction: column;
		gap: 2px;
		min-width: 0;
		width: 100%;
	}

	.tile-name {
		font-weight: 700;
		letter-spacing: -0.01em;
		line-height: 1.2;
		overflow-wrap: anywhere;
	}

	.tile-uptime {
		font-weight: 600;
	}

	.tile-ago {
		font-size: 12px;
		opacity: 0.85;
	}
</style>
