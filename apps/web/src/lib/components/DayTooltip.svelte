<script lang="ts">
	import type { DayHistory, IncidentsResponse } from '$lib/api';
	import { fetchIncidentsCached } from '$lib/api';
	import { fmtDate, fmtMins, fmtTime } from '$lib/format';
	import Icon from './Icon.svelte';

	let { day, x, y, monitorId }: { day: DayHistory; x: number; y: number; monitorId: string } =
		$props();

	const OFFSET = 16;
	const MARGIN = 12;
	const INCIDENTS_DEBOUNCE_MS = 150;
	const MAX_INCIDENT_ROWS = 6;

	let tooltipEl = $state<HTMLDivElement>();
	let left = $state(x + OFFSET);
	let top = $state(y + OFFSET);

	// The last successfully loaded incidents, plus the `id|date` (no
	// `checks`) it belongs to -- kept across a same-day refetch (the day in
	// progress gets a new `checks` count roughly every minute) so the
	// tooltip doesn't blank out to a loading state and flicker every time.
	let incidentsState = $state<IncidentsResponse | null>(null);
	let incidentsBaseKey = $state<string | null>(null);

	const shouldFetchIncidents = $derived(day.downMinutes > 0 || day.degradedMinutes > 0);
	const baseKey = $derived(`${monitorId}|${day.date}`);
	const fullKey = $derived(`${baseKey}|${day.checks}`);

	// Debounced (sweeping the mouse across many bars shouldn't fire a
	// request per bar) and self-discarding: if a newer key becomes current
	// before this fetch resolves -- a different day, or the same day's
	// `checks` moving on -- its response is dropped instead of rendered.
	$effect(() => {
		if (!shouldFetchIncidents) {
			incidentsState = null;
			incidentsBaseKey = null;
			return;
		}
		if (incidentsBaseKey !== null && incidentsBaseKey !== baseKey) {
			incidentsState = null;
			incidentsBaseKey = null;
		}

		const key = fullKey;
		const timer = setTimeout(() => {
			fetchIncidentsCached(monitorId, day.date, day.checks)
				.then((resp) => {
					if (fullKey !== key) return;
					incidentsState = resp;
					incidentsBaseKey = baseKey;
				})
				.catch(() => {
					// Keep whatever was already displayed; the aggregate
					// minutes above still convey the day had trouble.
				});
		}, INCIDENTS_DEBOUNCE_MS);
		return () => clearTimeout(timer);
	});

	// Flip to the opposite side of the cursor whenever the default offset
	// would push the tooltip past the viewport edge, instead of letting it
	// render off-screen and get clipped.
	$effect(() => {
		void day;
		void incidentsState; // async incident rows can change the tooltip's height after first render
		const el = tooltipEl;
		if (!el || typeof window === 'undefined') return;
		const width = el.offsetWidth;
		const height = el.offsetHeight;

		const overflowsRight = x + OFFSET + width > window.innerWidth - MARGIN;
		const overflowsBottom = y + OFFSET + height > window.innerHeight - MARGIN;

		left = Math.max(MARGIN, overflowsRight ? x - OFFSET - width : x + OFFSET);
		top = Math.max(MARGIN, overflowsBottom ? y - OFFSET - height : y + OFFSET);
	});

	const note = $derived.by(() => {
		if (day.state === 'none') return 'Sin datos registrados para este día.';
		if (day.checks > 0) return `${day.checks} verificaciones, ${day.fails} fallidas.`;
		return 'Sin caídas registradas este día.';
	});

	const visibleIncidents = $derived(incidentsState?.incidents.slice(0, MAX_INCIDENT_ROWS) ?? []);
</script>

<div class="tooltip" bind:this={tooltipEl} style="left: {left}px; top: {top}px;">
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

	{#if visibleIncidents.length > 0}
		<div class="tooltip-incidents">
			{#each visibleIncidents as incident (incident.start)}
				<div class="tooltip-row {incident.state === 'down' ? 'tooltip-row-bad' : 'tooltip-row-warn'}">
					<Icon name={incident.state === 'down' ? 'close-circle-fill' : 'error-warning-fill'} size={16} />
					<span class="tooltip-row-label">
						{#if incident.ongoing}
							desde {fmtTime(incident.start)} (en curso)
						{:else}
							{fmtTime(incident.start)} – {fmtTime(incident.end)}
						{/if}
					</span>
				</div>
			{/each}
		</div>
	{/if}
	{#if incidentsState?.partial}
		<span class="tooltip-note">Detalle horario parcial para este día.</span>
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

	.tooltip-incidents {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}

	.tooltip-incidents .tooltip-row-label {
		font-size: 12px;
		font-family: var(--font-mono);
	}

	.tooltip-note {
		font-size: 12px;
		color: var(--fg-2);
	}
</style>
