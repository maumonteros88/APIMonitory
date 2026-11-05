<script lang="ts">
	import { untrack } from 'svelte';
	import { UnauthorizedError, fetchLatency, type LatencyRange, type Monitor } from '$lib/api';
	import {
		DEFAULT_LATENCY_RANGE,
		LATENCY_RANGES,
		RANGE_INFO,
		REFRESH_MS,
		buildChartGroups,
		formatAge,
		parseLatencyRange,
		staleAfterMs,
		toLatencyData,
		type LatencyData
	} from '$lib/chart';
	import { WORST_DOT } from '$lib/filters';
	import LatencyCard from './LatencyCard.svelte';

	// monitors are the ones visible under the filters. tabVisible comes from
	// the page's visibilitychange handling: the refresh only runs while true.
	let { monitors, tabVisible }: { monitors: Monitor[]; tabVisible: boolean } = $props();

	const RANGE_KEY = 'apimonitory_chart_range';
	// How often the age notice re-evaluates while the tab is visible.
	const CLOCK_MS = 30000;

	function readRange(): LatencyRange {
		try {
			return parseLatencyRange(localStorage.getItem(RANGE_KEY));
		} catch {
			// Storage unavailable: 24 h for this tab.
			return DEFAULT_LATENCY_RANGE;
		}
	}

	let range = $state<LatencyRange>(readRange());
	// Raw: replaced wholesale on every response, never mutated in place.
	let data = $state.raw<LatencyData | null>(null);
	let loading = $state(false);
	let failed = $state(false);
	// A request is in flight (foreground or background).
	let busy = $state(false);
	// The most recent refresh failed: the data on screen is the last good one.
	let refreshFailed = $state(false);
	// When `data` arrived, and a clock for the age notice. Display state: the
	// logic below uses the plain lastSuccessAt.
	let updatedAt = $state(0);
	let now = $state(Date.now());

	const groups = $derived(buildChartGroups(monitors));
	// Sorted and joined, so the fetch effect below only reruns when the set
	// of visible monitors changes: an SSE delta replaces monitors with new
	// objects but leaves this string equal, and an equal derived does not
	// notify its dependents.
	const idKey = $derived(
		monitors
			.map((m) => m.id)
			.sort()
			.join(',')
	);
	// A response for another range must not sit under this range's caption.
	const current = $derived(data !== null && data.range === range ? data : null);
	// Stale: the last refresh failed, or the data is older than two refresh
	// intervals (checked only while no request is in flight, so a catch-up
	// refresh after returning to the tab does not flash the notice).
	const stale = $derived(
		current !== null && (refreshFailed || (!busy && now - updatedAt > staleAfterMs(range)))
	);

	// Plain variables, not state: the effects must not depend on them.
	let controller: AbortController | null = null;
	let lastSuccessAt = 0;

	// One request at a time: a new one aborts the previous, so a slow 30 d
	// response can never land after (and overwrite) a newer 24 h one. A
	// background refresh keeps the current data on screen; if it fails, the
	// header says the data is stale. A foreground load shows "Cargando…" or
	// the error in the cards that have nothing to draw.
	async function load(nextRange: LatencyRange, key: string, background: boolean) {
		controller?.abort();
		const own = new AbortController();
		controller = own;
		if (!background) loading = true;
		busy = true;
		try {
			const res = await fetchLatency(nextRange, key === '' ? [] : key.split(','), own.signal);
			if (own !== controller || res.range !== nextRange) return;
			data = toLatencyData(res);
			failed = false;
			refreshFailed = false;
			lastSuccessAt = Date.now();
			updatedAt = lastSuccessAt;
			now = lastSuccessAt;
		} catch (err) {
			if (own !== controller || own.signal.aborted) return;
			// The page's onUnauthorized listener drops back to the token
			// gate, which unmounts this view.
			if (err instanceof UnauthorizedError) return;
			console.error('failed to load latency', err);
			refreshFailed = true;
			now = Date.now();
			if (!background) failed = true;
		} finally {
			if (own === controller) {
				controller = null;
				loading = false;
				busy = false;
			}
		}
	}

	function refresh() {
		// A request already in flight is at least as fresh.
		if (controller) return;
		load(range, idKey, current !== null);
	}

	// The cards' retry (nothing to draw) loads in the foreground; the stale
	// notice's retry keeps the last data on screen while it runs.
	function retry() {
		load(range, idKey, false);
	}

	function retryStale() {
		load(range, idKey, true);
	}

	function setRange(next: LatencyRange) {
		range = next;
		try {
			localStorage.setItem(RANGE_KEY, next);
		} catch {
			// Storage unavailable: the range still works for this tab.
		}
	}

	// Mount (the view becoming active), a range change and a change in the
	// visible ids each load once.
	$effect(() => {
		const nextRange = range;
		const key = idKey;
		untrack(() => load(nextRange, key, false));
	});

	$effect(() => {
		if (!tabVisible) return;
		// Each range refreshes at its own pace (see REFRESH_MS); a range
		// change reruns this effect with the new interval.
		const interval = REFRESH_MS[range];
		now = Date.now();
		// Back on a tab hidden for a while: catch up now, not an interval later.
		if (lastSuccessAt > 0 && Date.now() - lastSuccessAt >= interval) untrack(refresh);
		const handle = setInterval(refresh, interval);
		const clock = setInterval(() => (now = Date.now()), CLOCK_MS);
		return () => {
			clearInterval(handle);
			clearInterval(clock);
		};
	});

	$effect(() => {
		return () => controller?.abort();
	});
</script>

<section class="chart-view">
	<div class="chart-header">
		<div class="titles">
			<span class="title">Latencia y estado</span>
			<span class="caption">{RANGE_INFO[range].caption}</span>
		</div>
		<div class="controls">
			{#if stale}
				<div class="stale" role="status">
					<span>
						{refreshFailed ? 'No se pudo actualizar · datos' : 'Datos'} de hace {formatAge(now - updatedAt)}
					</span>
					<button type="button" class="stale-retry" disabled={busy} onclick={retryStale}>
						{busy ? 'Reintentando…' : 'Reintentar'}
					</button>
				</div>
			{/if}
			<div class="legend">
				<span class="legend-item">
					<span class="legend-line" style="background: var(--m-series-front)" aria-hidden="true"></span>Front
				</span>
				<span class="legend-item">
					<span class="legend-line" style="background: var(--m-series-api)" aria-hidden="true"></span>API
				</span>
				<span class="legend-separator" aria-hidden="true"></span>
				<span class="legend-item">
					<span class="legend-square" style="background: {WORST_DOT.up}" aria-hidden="true"></span>Operativa
				</span>
				<span class="legend-item">
					<span class="legend-square" style="background: {WORST_DOT.degraded}" aria-hidden="true"></span>Degradada
				</span>
				<span class="legend-item">
					<span class="legend-square" style="background: {WORST_DOT.down}" aria-hidden="true"></span>Sin respuesta
				</span>
			</div>
			<div class="range-switch" role="group" aria-label="Período">
				{#each LATENCY_RANGES as option (option)}
					<button
						type="button"
						class="range-option"
						class:active={range === option}
						aria-pressed={range === option}
						onclick={() => setRange(option)}
					>
						{RANGE_INFO[option].label}
					</button>
				{/each}
			</div>
		</div>
	</div>

	{#each groups as group (group.key)}
		<section class="group">
			<div class="group-header">
				<span class="group-dot" style="background: {WORST_DOT[group.worst]}" aria-hidden="true"></span>
				<h2 class:unassigned={group.service === null}>{group.service ?? 'Sin servicio'}</h2>
				<span class="group-meta tone-{group.meta.tone}">{group.meta.text}</span>
			</div>
			<div class="cards">
				{#each group.cards as card (card.key)}
					<LatencyCard
						{card}
						service={group.service}
						{range}
						data={current}
						{loading}
						{failed}
						onRetry={retry}
					/>
				{/each}
			</div>
		</section>
	{/each}
</section>

<style>
	.chart-view {
		display: flex;
		flex-direction: column;
		gap: 24px;
	}

	.chart-header {
		display: flex;
		justify-content: space-between;
		align-items: center;
		gap: 16px;
		flex-wrap: wrap;
	}

	.titles {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}

	.title {
		font-size: 16px;
		font-weight: 700;
	}

	.caption {
		font-size: 12px;
		color: var(--m-sub-tx);
	}

	.controls {
		display: flex;
		align-items: center;
		gap: 12px 20px;
		flex-wrap: wrap;
	}

	.stale {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 12px;
		font-weight: 600;
		color: var(--m-warn-ink);
	}

	.stale-retry {
		padding: 4px 10px;
		border: 1px solid currentColor;
		border-radius: 4px;
		background: transparent;
		color: inherit;
		font-size: 12px;
		font-weight: 700;
		cursor: pointer;
	}

	.stale-retry:disabled {
		cursor: default;
		opacity: 0.7;
	}

	.stale-retry:focus-visible {
		outline: 2px solid var(--m-ink);
		outline-offset: 2px;
	}

	.legend {
		display: flex;
		align-items: center;
		gap: 8px 16px;
		flex-wrap: wrap;
		font-size: 12px;
		color: var(--m-sub-tx);
	}

	.legend-item {
		display: flex;
		align-items: center;
		gap: 6px;
	}

	.legend-line {
		width: 16px;
		height: 2px;
		border-radius: 1px;
	}

	.legend-square {
		width: 10px;
		height: 10px;
		border-radius: 2px;
	}

	.legend-separator {
		width: 1px;
		height: 16px;
		background: var(--border-default);
	}

	.range-switch {
		display: flex;
		height: 36px;
		box-sizing: border-box;
		padding: 3px;
		gap: 2px;
		border: 1px solid var(--border-default);
		border-radius: 4px;
		background: var(--m-surface);
	}

	.range-option {
		padding: 0 12px;
		border: 0;
		border-radius: 2px;
		background: transparent;
		color: var(--m-sub-tx);
		font-size: 13px;
		font-weight: 700;
		white-space: nowrap;
		cursor: pointer;
	}

	.range-option.active {
		background: var(--m-ink);
		color: var(--m-surface);
	}

	.range-option:focus-visible {
		outline: 2px solid var(--m-ink);
		outline-offset: 2px;
	}

	.group {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}

	.group-header {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
	}

	.group-dot {
		width: 10px;
		height: 10px;
		border-radius: 50%;
		flex-shrink: 0;
	}

	.group-header h2 {
		margin: 0;
		font-size: 18px;
		font-weight: 700;
		line-height: 1.5;
	}

	.group-header h2.unassigned {
		color: var(--m-sub-tx);
	}

	.group-meta {
		font-size: 12px;
		color: var(--m-sub-tx);
	}

	.group-meta.tone-bad {
		color: var(--m-bad-ink);
	}

	.group-meta.tone-warn {
		color: var(--m-warn-ink);
	}

	/* min() keeps a single full-width column on a phone instead of a 440px
	   column that would scroll the page sideways. */
	.cards {
		display: grid;
		grid-template-columns: repeat(auto-fill, minmax(min(440px, 100%), 1fr));
		gap: 16px;
		align-items: start;
	}
</style>
