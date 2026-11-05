<script lang="ts">
	import type { LatencyPoint, LatencyRange, LatencyStatus } from '$lib/api';
	import {
		RANGE_INFO,
		bucketDate,
		buildSeries,
		cardState,
		cardSummary,
		chartAriaLabel,
		downBucketsLabel,
		formatAxisMs,
		formatBucketTime,
		formatMs,
		lastValue,
		lineRuns,
		niceMax,
		peakOf,
		pointLabel,
		polylinePoints,
		spreadLabels,
		summarize,
		xOf,
		yOf,
		type ChartCard,
		type LatencyData,
		type Series
	} from '$lib/chart';
	import { WORST_DOT } from '$lib/filters';
	import { ENV_LABELS } from '$lib/grouping';

	// data is the last response for the current range, or null; loading and
	// failed describe the latest request (see cardState).
	let {
		card,
		service,
		range,
		data,
		loading,
		failed,
		onRetry
	}: {
		card: ChartCard;
		service: string | null;
		range: LatencyRange;
		data: LatencyData | null;
		loading: boolean;
		failed: boolean;
		onRetry: () => void;
	} = $props();

	const PLOT_HEIGHT = 140;
	const COLUMN_GAP = 8;
	// End labels closer than this (in plot percent, ~20px) get pushed apart.
	const LABEL_GAP = 14;
	const NO_POINTS: LatencyPoint[] = [];

	let showTable = $state(false);
	let hover = $state<number | null>(null);
	let plotWidth = $state(0);
	let tipWidth = $state(0);

	const phase = $derived(
		cardState(
			card.monitors.map((m) => m.id),
			data,
			loading,
			failed
		)
	);
	const series = $derived(buildSeries(card.monitors));
	const pointsOf = $derived(series.map((s) => data?.byId.get(s.id) ?? NO_POINTS));
	const values = $derived(pointsOf.map((points) => points.map((p) => p.avgMs)));
	const buckets = $derived(Math.max(RANGE_INFO[range].buckets, ...pointsOf.map((points) => points.length)));
	const max = $derived(niceMax(peakOf(values)));
	const runs = $derived(values.map((v) => lineRuns(v, max, buckets)));
	const summaries = $derived(pointsOf.map(summarize));
	const ariaLabel = $derived(chartAriaLabel(service, card.environment, series, range));

	// Monitor names instead of "Front"/"API" need wider label columns.
	const named = $derived(series.some((s) => s.dash !== null));
	const labelColumn = $derived(named ? 104 : 56);
	const endColumn = $derived(named ? 104 : 40);

	const endLabels = $derived.by(() => {
		const placed = series
			.map((s, i) => ({ s, last: lastValue(values[i]) }))
			.filter((item): item is { s: Series; last: number } => item.last !== null);
		const tops = spreadLabels(
			placed.map((item) => yOf(item.last, max)),
			LABEL_GAP,
			4,
			96
		);
		return placed.map((item, i) => ({ s: item.s, top: tops[i] }));
	});

	// A range with fewer buckets can leave the last hover out of bounds.
	const hoverIndex = $derived(phase === 'ready' && hover !== null && hover < buckets ? hover : null);
	const hoverLeft = $derived(hoverIndex === null ? 0 : xOf(hoverIndex, buckets));

	// Pixels, so the tooltip can flip past 60 % of the width and still stay
	// inside the card (over the axis columns if needed) at phone widths.
	const tipLeft = $derived.by(() => {
		const x = (hoverLeft / 100) * plotWidth;
		const wanted = hoverLeft > 60 ? x - 12 - tipWidth : x + 12;
		const min = -(labelColumn + COLUMN_GAP);
		const max = plotWidth + COLUMN_GAP + endColumn - tipWidth;
		return Math.max(min, Math.min(max, wanted));
	});

	// 1px separators between status cells only while cells stay wide enough
	// to read; at phone width they would eat most of the strip.
	const stripGap = $derived(plotWidth / buckets >= 4 ? 1 : 0);

	const STATUS_LABELS: Record<LatencyStatus, string> = {
		up: 'Operativa',
		degraded: 'Degradada',
		down: 'Sin respuesta',
		none: 'Sin datos'
	};

	function color(s: Series): string {
		return `var(--m-series-${s.kind})`;
	}

	function statusAt(points: LatencyPoint[], index: number): LatencyStatus {
		return points[index]?.status ?? 'none';
	}

	function handlePointerMove(event: PointerEvent) {
		const rect = (event.currentTarget as HTMLElement).getBoundingClientRect();
		const fraction = (event.clientX - rect.left) / rect.width;
		// Each bucket owns a full-height column centered on its point.
		hover = Math.max(0, Math.min(buckets - 1, Math.round(fraction * (buckets - 1))));
	}
</script>

{#snippet swatch(s: Series)}
	<svg class="swatch" width="16" height="4" viewBox="0 0 16 4" aria-hidden="true">
		<line x1="2" y1="2" x2="14" y2="2" stroke={color(s)} stroke-dasharray={s.dash ?? undefined} />
	</svg>
{/snippet}

<article
	class="card"
	style="--label-col: {labelColumn}px; --end-col: {endColumn}px; --col-gap: {COLUMN_GAP}px"
	aria-busy={phase === 'loading'}
>
	<div class="card-header">
		<span class="env-badge env-{card.environment ?? 'none'}">
			{card.environment ? ENV_LABELS[card.environment] : 'Sin ambiente'}
		</span>
		<span class="summary">{phase === 'ready' ? cardSummary(series, summaries) : ''}</span>
		<button type="button" class="toggle" disabled={phase !== 'ready'} onclick={() => (showTable = !showTable)}>
			{showTable ? 'Ver gráfico' : 'Ver tabla'}
		</button>
	</div>

	{#if phase !== 'ready'}
		<div class="placeholder" style="height: {PLOT_HEIGHT + 24 + series.length * 16}px">
			{#if phase === 'loading'}
				<span>Cargando…</span>
			{:else}
				<span>No se pudo cargar la latencia</span>
				<button type="button" class="retry" onclick={onRetry}>Reintentar</button>
			{/if}
		</div>
	{:else if showTable}
		<div class="table-wrap">
			<table>
				<caption class="visually-hidden">{ariaLabel}</caption>
				<thead>
					<tr>
						<th scope="col">Monitor</th>
						<th scope="col" class="num">Promedio</th>
						<th scope="col" class="num">Máximo</th>
						<th scope="col" class="num">Intervalos con caída</th>
					</tr>
				</thead>
				<tbody>
					{#each series as s, i (s.id)}
						{@const summary = summaries[i]}
						<tr>
							<th scope="row">
								<span class="row-label">{@render swatch(s)}{s.label}</span>
							</th>
							<td class="num">{summary.avgMs === null ? 'Sin datos' : formatMs(summary.avgMs)}</td>
							<td class="num">{summary.maxMs === null ? 'Sin datos' : formatMs(summary.maxMs)}</td>
							<td class="num">{downBucketsLabel(summary)}</td>
						</tr>
					{/each}
				</tbody>
			</table>
		</div>
	{:else}
		<div class="chart">
			<div class="row" style="height: {PLOT_HEIGHT}px">
				<div class="y-axis" aria-hidden="true">
					<span class="y-top">{formatAxisMs(max)}</span>
					<span>{formatAxisMs(max / 2)}</span>
					<span class="y-bottom">0 ms</span>
				</div>

				<div
					class="plot"
					role="img"
					aria-label={ariaLabel}
					bind:clientWidth={plotWidth}
					onpointermove={handlePointerMove}
					onpointerleave={() => (hover = null)}
				>
					<svg class="lines" viewBox="0 0 100 100" preserveAspectRatio="none" aria-hidden="true">
						<line class="grid" x1="0" y1="0" x2="100" y2="0" vector-effect="non-scaling-stroke" />
						<line class="grid" x1="0" y1="50" x2="100" y2="50" vector-effect="non-scaling-stroke" />
						<line class="baseline" x1="0" y1="100" x2="100" y2="100" vector-effect="non-scaling-stroke" />
						{#each series as s, i (s.id)}
							{#each runs[i] as run, r (r)}
								{#if run.length > 1}
									<polyline
										points={polylinePoints(run)}
										stroke={color(s)}
										stroke-dasharray={s.dash ?? undefined}
										stroke-width="2"
										stroke-linejoin="round"
										stroke-linecap="round"
										fill="none"
										vector-effect="non-scaling-stroke"
									/>
								{/if}
							{/each}
						{/each}
					</svg>

					<!-- A value between two gaps has no neighbor to join: a dot
					     keeps it visible. HTML, since the SVG's non-uniform
					     scale would turn a circle into an oval. -->
					{#each series as s, i (s.id)}
						{#each runs[i] as run, r (r)}
							{#if run.length === 1}
								<span
									class="lone-point"
									style="left: {run[0].x}%; top: {run[0].y}%; background: {color(s)}"
									aria-hidden="true"
								></span>
							{/if}
						{/each}
					{/each}

					{#if hoverIndex !== null}
						<div class="hover" aria-hidden="true">
							<span class="crosshair" style="left: {hoverLeft}%"></span>
							{#each series as s, i (s.id)}
								{@const value = pointsOf[i][hoverIndex]?.avgMs ?? null}
								{#if value !== null}
									<span class="hover-dot" style="left: {hoverLeft}%; top: {yOf(value, max)}%; background: {color(s)}"
									></span>
								{/if}
							{/each}
							<div class="tip" style="left: {tipLeft}px" bind:offsetWidth={tipWidth}>
								<span class="tip-time">
									{data ? formatBucketTime(bucketDate(data.from, data.bucketMinutes, hoverIndex), range) : ''}
								</span>
								{#each series as s, i (s.id)}
									<span class="tip-row">
										{@render swatch(s)}
										<span class="tip-name">{s.label}</span>
										<span class="tip-value">{pointLabel(pointsOf[i][hoverIndex])}</span>
									</span>
								{/each}
							</div>
						</div>
					{/if}
				</div>

				<div class="end-labels" aria-hidden="true">
					{#each endLabels as label (label.s.id)}
						<span class="end-label" style="top: {label.top}%" title={label.s.label}>{label.s.label}</span>
					{/each}
				</div>
			</div>

			<div class="row x-axis" aria-hidden="true">
				<span></span>
				<div class="x-labels">
					<span>{RANGE_INFO[range].xStart}</span>
					{#if plotWidth >= 200}<span>{RANGE_INFO[range].xMid}</span>{/if}
					<span>Ahora</span>
				</div>
				<span></span>
			</div>

			{#each series as s, i (s.id)}
				<div class="row strip-row">
					<span class="strip-label" title={s.label}>{@render swatch(s)}<span class="strip-name">{s.label}</span></span>
					<div class="strip" role="img" aria-label="Estado de {s.label} en cada intervalo" style="gap: {stripGap}px">
						{#each { length: buckets }, b}
							<span class="cell" style="background: {WORST_DOT[statusAt(pointsOf[i], b)]}"></span>
						{/each}
					</div>
					<span></span>
				</div>
			{/each}
		</div>
	{/if}
</article>

<style>
	.card {
		display: flex;
		flex-direction: column;
		gap: 12px;
		min-width: 0;
		padding: 16px;
		background: var(--m-surface);
		border: 1px solid var(--border-default);
		border-radius: 8px;
	}

	.card-header {
		display: flex;
		align-items: center;
		gap: 8px 10px;
		flex-wrap: wrap;
	}

	.env-badge {
		flex-shrink: 0;
		font-size: 12px;
		font-weight: 600;
		line-height: 1.5;
		padding: 0 6px;
		border-radius: 4px;
		border: 1px solid var(--border-default);
		background: var(--m-surface);
		color: var(--m-sub-tx);
	}

	.env-prod {
		border-color: var(--m-ink);
		background: var(--m-ink);
		color: var(--m-surface);
	}

	.env-qa {
		border-color: var(--border-subtle);
		background: var(--border-subtle);
		color: var(--fg-1);
	}

	.env-prepro {
		border-color: var(--fg-2);
		color: var(--fg-1);
	}

	.env-dev {
		border-style: dashed;
		border-color: var(--fg-3);
	}

	.summary {
		flex: 1;
		min-width: 0;
		font-size: 12px;
		color: var(--m-sub-tx);
	}

	.toggle,
	.retry {
		height: 28px;
		padding: 0 10px;
		border: 1px solid var(--border-default);
		border-radius: 4px;
		background: none;
		color: var(--fg-1);
		font-size: 12px;
		font-weight: 600;
		white-space: nowrap;
		cursor: pointer;
	}

	.toggle:hover:not(:disabled),
	.retry:hover {
		background: var(--m-soft);
	}

	.toggle:disabled {
		color: var(--m-sub-tx);
		cursor: default;
	}

	.toggle:focus-visible,
	.retry:focus-visible {
		outline: 2px solid var(--m-ink);
		outline-offset: 2px;
	}

	.placeholder {
		display: flex;
		flex-direction: column;
		align-items: center;
		justify-content: center;
		gap: 10px;
		border-radius: 4px;
		background: var(--m-soft);
		font-size: 12px;
		color: var(--m-sub-tx);
	}

	.chart {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.row {
		display: grid;
		grid-template-columns: var(--label-col) minmax(0, 1fr) var(--end-col);
		gap: var(--col-gap);
	}

	.y-axis {
		display: flex;
		flex-direction: column;
		justify-content: space-between;
		align-items: flex-end;
		font-size: 11px;
		color: var(--m-sub-tx);
		white-space: nowrap;
	}

	.y-top {
		transform: translateY(-50%);
	}

	.y-bottom {
		transform: translateY(50%);
	}

	.plot {
		position: relative;
		touch-action: pan-y;
	}

	.lines {
		position: absolute;
		inset: 0;
		width: 100%;
		height: 100%;
		overflow: visible;
	}

	.grid {
		stroke: var(--border-subtle);
		stroke-width: 1;
	}

	.baseline {
		stroke: var(--border-default);
		stroke-width: 1;
	}

	.lone-point {
		position: absolute;
		width: 6px;
		height: 6px;
		border-radius: 50%;
		transform: translate(-50%, -50%);
		pointer-events: none;
	}

	.hover {
		position: absolute;
		inset: 0;
		pointer-events: none;
	}

	.crosshair {
		position: absolute;
		top: 0;
		bottom: 0;
		width: 1px;
		background: var(--fg-3);
	}

	.hover-dot {
		position: absolute;
		width: 10px;
		height: 10px;
		border-radius: 50%;
		transform: translate(-50%, -50%);
		box-shadow: 0 0 0 2px var(--m-surface);
	}

	.tip {
		position: absolute;
		top: 4px;
		z-index: 5;
		min-width: 168px;
		max-width: 280px;
		box-sizing: border-box;
		padding: 10px 12px;
		display: flex;
		flex-direction: column;
		gap: 6px;
		background: var(--m-surface);
		border: 1px solid var(--border-default);
		border-radius: 6px;
		box-shadow: 0 12px 28px -6px var(--m-shadow);
	}

	.tip-time {
		font-size: 12px;
		font-weight: 700;
	}

	.tip-row {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 12px;
	}

	.tip-name {
		flex: 1;
		min-width: 0;
		color: var(--m-sub-tx);
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.tip-value {
		font-weight: 700;
		white-space: nowrap;
	}

	.end-labels {
		position: relative;
	}

	.end-label {
		position: absolute;
		left: 0;
		max-width: 100%;
		transform: translateY(-50%);
		font-size: 11px;
		font-weight: 600;
		color: var(--fg-1);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.x-axis {
		font-size: 11px;
		color: var(--m-sub-tx);
	}

	.x-labels {
		display: flex;
		justify-content: space-between;
		gap: 8px;
		white-space: nowrap;
	}

	.strip-row {
		align-items: center;
	}

	.strip-label {
		display: flex;
		align-items: center;
		justify-content: flex-end;
		gap: 4px;
		min-width: 0;
		font-size: 11px;
		color: var(--m-sub-tx);
	}

	.strip-name {
		min-width: 0;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}

	.swatch {
		flex-shrink: 0;
	}

	.swatch line {
		stroke-width: 2;
		stroke-linecap: round;
	}

	.strip {
		display: flex;
		height: 8px;
	}

	.cell {
		flex: 1;
		min-width: 0;
		border-radius: 1px;
	}

	.table-wrap {
		overflow-x: auto;
	}

	table {
		width: 100%;
		border-collapse: collapse;
		font-size: 13px;
	}

	thead th {
		padding: 6px 8px;
		border-bottom: 1px solid var(--border-default);
		font-size: 12px;
		font-weight: 600;
		color: var(--m-sub-tx);
		text-align: left;
	}

	tbody th,
	tbody td {
		padding: 8px;
		border-bottom: 1px solid var(--border-subtle);
		font-weight: 400;
		text-align: left;
	}

	.row-label {
		display: inline-flex;
		align-items: center;
		gap: 6px;
	}

	/* Headers may wrap ("Intervalos con caída") so the table fits a phone;
	   values never do. */
	th.num,
	td.num {
		text-align: right;
	}

	td.num {
		white-space: nowrap;
	}

	.visually-hidden {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		clip: rect(0 0 0 0);
		white-space: nowrap;
	}
</style>
