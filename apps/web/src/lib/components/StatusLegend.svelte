<script lang="ts">
	import { dayColor, statusMeta, type DayState, type MonitorStatus } from '$lib/format';

	// Always-visible key to the colors used across the page. The first group
	// is the current state of a service (dots, tiles, banners); the second is
	// the daily bars, which have their own thresholds. Colors come from
	// format.ts so the key can't drift from what the page paints.
	// detail is the longer explanation, shown from the "wide" breakpoint up.
	const CURRENT: { status: MonitorStatus; detail: string }[] = [
		{ status: 'up', detail: 'responde bien' },
		{ status: 'degraded', detail: 'responde, pero lento' },
		{ status: 'down', detail: 'falló varias veces seguidas' }
	];

	const DAYS: { state: DayState; label: string; detail: string }[] = [
		{ state: 'op', label: 'Sin cortes', detail: '' },
		{ state: 'deg', label: 'Lentitud', detail: 'respondió lento' },
		{ state: 'part', label: 'Caída parcial', detail: 'menos de 1 h' },
		{ state: 'major', label: 'Caída', detail: '1 h o más' },
		{ state: 'none', label: 'Sin datos', detail: '' }
	];
</script>

<aside class="legend" aria-label="Referencia de colores">
	<div class="group" role="group" aria-labelledby="legend-now">
		<span id="legend-now" class="group-title">Ahora</span>
		<ul role="list">
			{#each CURRENT as item (item.status)}
				<li>
					<span class="dot" style="background: {statusMeta(item.status).dotColor}" aria-hidden="true"></span>
					<span class="label">{statusMeta(item.status).label}</span>
					<span class="detail">· {item.detail}</span>
				</li>
			{/each}
		</ul>
	</div>

	<div class="group days" role="group" aria-labelledby="legend-days">
		<span id="legend-days" class="group-title">Barras por día</span>
		<ul role="list">
			{#each DAYS as item (item.state)}
				<li>
					<span class="swatch" style="background: {dayColor(item.state)}" aria-hidden="true"></span>
					<span class="label">{item.label}</span>
					{#if item.detail}
						<span class="detail">· {item.detail}</span>
					{/if}
				</li>
			{/each}
		</ul>
	</div>
</aside>

<style>
	.legend {
		position: sticky;
		top: 0;
		/* Above the cards that scroll under it (z-index 5 to 10), below menus. */
		z-index: 15;
		display: flex;
		flex-direction: column;
		gap: 6px;
		padding: 10px 16px;
		border: 1px solid var(--border-default);
		border-radius: 8px;
		background: var(--m-surface);
		box-shadow: 0 6px 12px -6px var(--m-shadow);
		font-size: 13px;
		color: var(--fg-1);
	}

	.group {
		display: flex;
		flex-wrap: wrap;
		align-items: center;
		gap: 4px 16px;
		min-width: 0;
	}

	.group-title {
		min-width: 112px;
		font-size: 12px;
		font-weight: 700;
		letter-spacing: 0.04em;
		text-transform: uppercase;
		color: var(--fg-2);
	}

	ul {
		display: flex;
		flex-wrap: wrap;
		gap: 4px 20px;
		margin: 0;
		padding: 0;
		list-style: none;
	}

	li {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		white-space: nowrap;
	}

	.dot {
		width: 10px;
		height: 10px;
		border-radius: 50%;
		flex: none;
	}

	.swatch {
		width: 6px;
		height: 16px;
		border-radius: 2px;
		flex: none;
	}

	.label {
		font-weight: 600;
	}

	.detail {
		color: var(--fg-2);
	}

	/* Without room for the explanations the key is just labels. */
	.detail {
		display: none;
	}

	/* From here both groups share one set of columns, so the first item of
	   each row lines up, then the second, and so on. */
	@media (min-width: 900px) {
		.legend {
			display: grid;
			grid-template-columns: max-content repeat(5, max-content);
			justify-content: start;
			align-items: center;
			column-gap: 32px;
			row-gap: 8px;
		}

		.group,
		ul {
			display: contents;
		}

		.group-title {
			grid-column: 1;
			min-width: 0;
		}
	}

	@media (min-width: 1280px) {
		.detail {
			display: inline;
		}
	}

	@media (max-width: 639px) {
		.legend {
			padding: 8px 12px;
			border-radius: 0;
			border-inline: 0;
		}

		.days,
		.group-title {
			display: none;
		}

		ul {
			gap: 4px 12px;
		}
	}
</style>
