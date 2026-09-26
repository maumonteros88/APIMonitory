<script lang="ts">
	import type { Monitor } from '$lib/api';
	import Icon, { type IconName } from './Icon.svelte';

	let { monitors, intervalSeconds }: { monitors: Monitor[]; intervalSeconds: number } = $props();

	type Level = 'none' | 'ok' | 'warn' | 'bad';

	const level: Level = $derived.by(() => {
		if (monitors.length === 0) return 'none';
		if (monitors.some((m) => m.status === 'down')) return 'bad';
		if (monitors.some((m) => m.status === 'degraded')) return 'warn';
		return 'ok';
	});

	const icon: IconName = $derived(
		level === 'bad'
			? 'close-circle-fill'
			: level === 'warn'
				? 'error-warning-fill'
				: level === 'ok'
					? 'checkbox-circle-fill'
					: 'pulse-line'
	);

	const text = $derived.by(() => {
		if (level === 'bad') {
			const n = monitors.filter((m) => m.status === 'down').length;
			return `${n} API${n === 1 ? '' : 's'} sin respuesta`;
		}
		if (level === 'warn') {
			const n = monitors.filter((m) => m.status === 'degraded').length;
			return `${n} API${n === 1 ? '' : 's'} con rendimiento degradado`;
		}
		if (level === 'ok') return 'Todas las APIs operativas';
		return 'Sin APIs configuradas';
	});

	const intervalLabel = $derived(
		intervalSeconds >= 60 && intervalSeconds % 60 === 0
			? `${intervalSeconds / 60} min`
			: `${intervalSeconds} s`
	);

	const sub = $derived(
		monitors.length === 0
			? ''
			: `${monitors.length} API${monitors.length === 1 ? '' : 's'} monitoreada${monitors.length === 1 ? '' : 's'} · se verifica cada ${intervalLabel}`
	);
</script>

<div class="banner banner-{level}">
	<Icon name={icon} />
	<span class="banner-text">{text}</span>
	{#if sub}<span class="banner-sub">{sub}</span>{/if}
</div>

<style>
	.banner {
		padding: 16px 20px;
		border-radius: 8px;
		display: flex;
		align-items: center;
		gap: 12px;
		flex-wrap: wrap;
	}

	.banner-text {
		font-size: 16px;
		font-weight: 700;
	}

	.banner-sub {
		font-size: 12px;
		color: var(--fg-2);
	}

	.banner-ok {
		background: var(--m-ok-bg);
		border: 1px solid var(--m-ok-bd);
	}
	.banner-ok :global(svg) {
		color: #289b4f;
	}

	.banner-warn {
		background: var(--m-warn-bg);
		border: 1px solid var(--m-warn-bd);
	}
	.banner-warn :global(svg) {
		color: #e0a800;
	}

	.banner-bad {
		background: var(--m-bad-bg);
		border: 1px solid var(--m-bad-bd);
	}
	.banner-bad :global(svg) {
		color: #df4730;
	}

	.banner-none {
		background: var(--m-surface);
		border: 1px solid var(--border-default);
	}
	.banner-none :global(svg) {
		color: var(--fg-2);
	}
</style>
