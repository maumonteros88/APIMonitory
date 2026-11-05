<script lang="ts">
	import type { Environment, Monitor, MonitorKind } from '$lib/api';
	import { ago, statusMeta, tileStyle, uptimeShort } from '$lib/format';
	import {
		ENV_LABELS,
		KIND_LABELS,
		MATRIX_ENVS,
		buildBoard,
		buildMatrix,
		countLabel,
		type EnvFilter,
		type Segment
	} from '$lib/grouping';
	import type { Period } from '$lib/period';
	import Icon from './Icon.svelte';
	import PeriodSelect from './PeriodSelect.svelte';

	// monitors are the ones visible under the filters, already carrying the
	// selected period's uptime. Under 'all' the board is a services ×
	// environments matrix; otherwise one card per service. A missing kind or
	// an empty cell offers onAdd, prefilled from where it was clicked.
	let {
		monitors,
		env,
		days,
		onDaysChange,
		onCheck,
		onAdd
	}: {
		monitors: Monitor[];
		env: EnvFilter;
		days: Period;
		onDaysChange: (days: Period) => void;
		onCheck: (id: string) => void;
		onAdd: (service: string, kind: MonitorKind, environment: Environment) => void;
	} = $props();

	const boardEnv = $derived(env === 'all' ? null : env);
	const board = $derived(boardEnv ? buildBoard(monitors) : null);
	const matrix = $derived(env === 'all' ? buildMatrix(monitors) : null);

	function latency(m: Monitor): string {
		if (m.checking) return 'Verificando…';
		return m.lastCheck ? `${m.lastCheck.latencyMs} ms` : '';
	}

	function lastAgo(m: Monitor): string {
		if (m.checking) return 'Verificando…';
		return m.lastCheck ? `Verificada ${ago(m.lastCheck.at)}` : 'Sin verificar';
	}

	function segmentKey(seg: Segment): string {
		return seg.monitor?.id ?? `missing-${seg.kind}`;
	}
</script>

{#snippet segment(seg: Segment, compact: boolean, service: string, cellEnv: Environment)}
	{#if seg.monitor}
		{@const m = seg.monitor}
		{@const style = tileStyle(m.status, m.uptimePercent)}
		<button
			type="button"
			class="segment"
			class:compact
			title="Verificar ahora"
			style="background: {style.bg}; color: {style.fg}"
			onclick={() => onCheck(m.id)}
		>
			<span class="segment-info">
				<span class="segment-kind">{KIND_LABELS[seg.kind]}</span>
				<span class="segment-status">{statusMeta(m.status).label}</span>
			</span>
			<span class="segment-stats">
				<span class="segment-uptime">{uptimeShort(m.uptimePercent)}</span>
				<span class="segment-latency">{latency(m)}</span>
			</span>
		</button>
	{:else}
		{@const kindWord = seg.kind === 'front' ? 'front' : 'API'}
		<!-- The hidden suffix tells apart the many "Agregar API" buttons for a
		     screen reader without changing what is visible. -->
		<button type="button" class="segment-add" class:compact onclick={() => onAdd(service, seg.kind, cellEnv)}>
			<Icon name="add-line" size={compact ? 16 : 18} />
			<span>Agregar {kindWord}<span class="visually-hidden"> · {service} · {ENV_LABELS[cellEnv]}</span></span>
		</button>
	{/if}
{/snippet}

{#snippet tiles(title: string, list: Monitor[])}
	<section class="unassigned">
		<div class="section-header">
			<h2>{title}</h2>
			<span class="meta">{countLabel(list.length)}</span>
		</div>
		<div class="tiles">
			{#each list as m (m.id)}
				{@const style = tileStyle(m.status, m.uptimePercent)}
				<button
					type="button"
					class="tile"
					title="Verificar ahora"
					style="background: {style.bg}; color: {style.fg}"
					onclick={() => onCheck(m.id)}
				>
					<div class="tile-top">
						<span class="tile-status">{KIND_LABELS[m.kind]} · {statusMeta(m.status).label}</span>
						<span class="tile-latency">{m.lastCheck ? `${m.lastCheck.latencyMs} ms` : ''}</span>
					</div>
					<div class="tile-mid">
						<span class="tile-name">{m.name}</span>
						<span class="tile-uptime">{uptimeShort(m.uptimePercent)}</span>
					</div>
					<span class="tile-ago">{lastAgo(m)}</span>
				</button>
			{/each}
		</div>
	</section>
{/snippet}

<section class="board">
	<div class="board-header">
		<div class="legend">
			<span class="legend-item"><span class="legend-dot" style="background: #289B4F"></span>Operativa</span>
			<span class="legend-item"><span class="legend-dot" style="background: #FFC700"></span>Degradada</span>
			<span class="legend-item"><span class="legend-dot" style="background: #DF4730"></span>Sin respuesta</span>
		</div>
		<PeriodSelect value={days} onChange={onDaysChange} />
	</div>

	{#if board && boardEnv}
		{#if board.cards.length > 0}
			<div class="cards">
				{#each board.cards as card (card.service)}
					<article class="card">
						<div class="section-header card-header">
							<h2>{card.service}</h2>
							<span class="meta">{card.meta}</span>
						</div>
						{#each card.segments as seg (segmentKey(seg))}
							{@render segment(seg, false, card.service, boardEnv)}
						{/each}
					</article>
				{/each}
			</div>
		{/if}
		{#if board.unassigned.length > 0}
			{@render tiles('Sin servicio', board.unassigned)}
		{/if}
	{:else if matrix}
		{#if matrix.rows.length > 0}
			<div class="matrix-scroll">
				<div class="matrix" style:--env-cols={MATRIX_ENVS.length}>
					<div class="matrix-row matrix-head">
						<span>Servicio</span>
						{#each MATRIX_ENVS as column (column)}
							<span>{ENV_LABELS[column].toUpperCase()}</span>
						{/each}
					</div>
					{#each matrix.rows as row (row.service)}
						<div class="matrix-row">
							<div class="matrix-service">
								<span class="matrix-name">{row.service}</span>
								<span class="meta">{row.meta}</span>
							</div>
							{#each row.cells as cell (cell.env)}
								{#if cell.segments.length === 0}
									<button type="button" class="cell-empty" onclick={() => onAdd(row.service, 'api', cell.env)}>
										<Icon name="add-line" size={16} />
										<span>
											Agregar en {ENV_LABELS[cell.env]}<span class="visually-hidden"> · {row.service}</span>
										</span>
									</button>
								{:else}
									<div class="cell-segments">
										{#each cell.segments as seg (segmentKey(seg))}
											{@render segment(seg, true, row.service, cell.env)}
										{/each}
									</div>
								{/if}
							{/each}
						</div>
					{/each}
				</div>
			</div>
		{/if}
		{#if matrix.unassigned.length > 0}
			{@render tiles('Sin servicio o ambiente', matrix.unassigned)}
		{/if}
	{/if}
</section>

<style>
	.board {
		display: flex;
		flex-direction: column;
		gap: 24px;
	}

	.board-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
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

	.section-header {
		display: flex;
		align-items: baseline;
		gap: 12px;
		flex-wrap: wrap;
	}

	.section-header h2 {
		margin: 0;
		font-size: 18px;
		font-weight: 700;
		line-height: 1.5;
	}

	.meta {
		font-size: 12px;
		color: var(--fg-2);
	}

	.cards {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
		gap: 8px;
	}

	/* The 2px gap between segments shows the card's own background as the
	   separator line. */
	.card {
		display: flex;
		flex-direction: column;
		gap: 2px;
		background: var(--m-surface);
		border: 1px solid var(--border-default);
		border-radius: 8px;
		overflow: hidden;
	}

	.card-header {
		justify-content: space-between;
		gap: 8px;
		padding: 12px 16px;
	}

	.segment {
		min-height: 80px;
		box-sizing: border-box;
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 12px;
		padding: 12px 16px;
		border: 0;
		text-align: left;
		cursor: pointer;
		transition: filter 150ms ease-out;
	}

	.segment:hover {
		filter: brightness(1.08);
	}

	.segment-info,
	.segment-stats {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}

	.segment-stats {
		align-items: flex-end;
		text-align: right;
	}

	.segment-kind {
		font-size: 12px;
		font-weight: 600;
		opacity: 0.9;
	}

	.segment-status {
		font-size: 14px;
		font-weight: 700;
	}

	.segment-uptime {
		font-size: 21px;
		font-weight: 700;
		letter-spacing: -0.01em;
		white-space: nowrap;
	}

	.segment-latency {
		font-size: 12px;
		opacity: 0.9;
		white-space: nowrap;
	}

	.segment-add {
		min-height: 80px;
		box-sizing: border-box;
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 12px 16px;
		border: 0;
		background: var(--m-soft);
		color: var(--fg-1);
		font-size: 14px;
		font-weight: 700;
		text-align: left;
		cursor: pointer;
	}

	.segment-add:hover {
		background: var(--border-default);
	}

	.segment-add :global(svg) {
		flex-shrink: 0;
	}

	.segment.compact,
	.segment-add.compact {
		min-height: 52px;
		padding: 8px 12px;
		gap: 8px;
	}

	/* Inside a matrix cell the missing kind reads as an empty slot. */
	.segment-add.compact {
		justify-content: center;
		gap: 6px;
		border: 1px dashed var(--border-default);
		border-radius: 2px;
		background: var(--m-surface);
		color: var(--m-sub-tx);
		font-size: 12px;
		font-weight: 600;
	}

	.segment-add.compact:hover {
		background: var(--m-soft);
		color: var(--fg-1);
	}

	.segment.compact .segment-info,
	.segment.compact .segment-stats {
		gap: 0;
	}

	.segment.compact .segment-status {
		font-size: 12px;
	}

	.segment.compact .segment-uptime {
		font-size: 16px;
		letter-spacing: 0;
	}

	.matrix-scroll {
		overflow-x: auto;
		background: var(--m-surface);
		border: 1px solid var(--border-default);
		border-radius: 8px;
	}

	.matrix {
		min-width: 800px;
		padding: 0 16px;
	}

	.matrix-row {
		display: grid;
		grid-template-columns: 168px repeat(var(--env-cols), minmax(0, 1fr));
		gap: 8px;
		padding: 12px 0;
		align-items: stretch;
	}

	.matrix-row + .matrix-row {
		border-top: 1px solid var(--border-subtle);
	}

	.matrix-head {
		font-size: 12px;
		font-weight: 600;
		letter-spacing: 0.04em;
		color: var(--fg-2);
	}

	.matrix-service {
		display: flex;
		flex-direction: column;
		align-self: start;
		gap: 2px;
		padding-top: 6px;
		min-width: 0;
	}

	.matrix-name {
		font-size: 16px;
		font-weight: 700;
		overflow-wrap: anywhere;
	}

	.cell-segments {
		display: flex;
		flex-direction: column;
		gap: 2px;
		border-radius: 4px;
		overflow: hidden;
		background: var(--m-surface);
	}

	/* Cells of a row share its height, so a cell's segments (or its add
	   slot) fill it instead of leaving a gap under a shorter cell. */
	.cell-segments > .segment,
	.cell-segments > .segment-add {
		flex: 1;
	}

	.cell-empty {
		min-height: 106px;
		box-sizing: border-box;
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 6px;
		padding: 0 12px;
		border: 1px dashed var(--border-default);
		border-radius: 4px;
		background: none;
		font-size: 12px;
		font-weight: 600;
		color: var(--m-sub-tx);
		cursor: pointer;
	}

	.cell-empty:hover {
		background: var(--m-soft);
		color: var(--fg-1);
	}

	/* Abspos needs a positioned ancestor inside the matrix's scroll box, or
	   a hidden suffix past the viewport edge widens the whole page. */
	.segment-add,
	.cell-empty {
		position: relative;
	}

	.visually-hidden {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		clip: rect(0 0 0 0);
		white-space: nowrap;
	}

	.unassigned {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.unassigned h2 {
		color: var(--fg-2);
	}

	.tiles {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(220px, 1fr));
		gap: 4px;
	}

	.tile {
		box-sizing: border-box;
		min-height: 170px;
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
		font-size: 21px;
		font-weight: 700;
		letter-spacing: -0.01em;
		line-height: 1.2;
		overflow-wrap: anywhere;
	}

	.tile-uptime {
		font-size: 16px;
		font-weight: 600;
	}

	.tile-ago {
		font-size: 12px;
		opacity: 0.85;
	}
</style>
