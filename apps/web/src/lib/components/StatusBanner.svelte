<script lang="ts">
	import type { Monitor } from '$lib/api';
	import { buildBanners, summaryLine, type Banner, type EnvFilter } from '$lib/grouping';
	import Icon, { type IconName } from './Icon.svelte';

	// monitors are the ones visible under env: hidden ones never count.
	let {
		monitors,
		env,
		intervalSeconds
	}: { monitors: Monitor[]; env: EnvFilter; intervalSeconds: number } = $props();

	const ICONS: Record<Banner['level'], IconName> = {
		bad: 'close-circle-fill',
		warn: 'error-warning-fill',
		ok: 'checkbox-circle-fill',
		none: 'pulse-line'
	};

	const banners = $derived(buildBanners(monitors, env));
	const summary = $derived(summaryLine(monitors.length, env, intervalSeconds));
</script>

<div class="banners">
	{#each banners as banner (banner.level)}
		<div class="banner banner-{banner.level}" role="status">
			<Icon name={ICONS[banner.level]} />
			<span class="banner-text">{banner.text}</span>
			{#if banner.items.length > 0}
				<span class="chips">
					{#each banner.items as item (item.id)}
						<span class="chip">{item.label}{#if item.env}<span class="chip-env">{item.env}</span>{/if}</span>
					{/each}
				</span>
			{/if}
		</div>
	{/each}
	{#if summary}<span class="summary">{summary}</span>{/if}
</div>

<style>
	.banners {
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.banner {
		padding: 16px 20px;
		border-radius: 8px;
		display: flex;
		align-items: center;
		gap: 12px;
		flex-wrap: wrap;
	}

	.banner :global(svg) {
		flex-shrink: 0;
	}

	.banner-text {
		font-size: 16px;
		font-weight: 700;
	}

	.chips {
		display: flex;
		gap: 6px;
		flex-wrap: wrap;
	}

	.chip {
		font-size: 12px;
		font-weight: 600;
		line-height: 1.5;
		padding: 2px 8px;
		border-radius: 4px;
		background: var(--m-surface);
		border: 1px solid var(--banner-border);
		color: var(--m-ink);
	}

	.chip-env {
		font-weight: 400;
		color: var(--fg-2);
	}

	.summary {
		font-size: 12px;
		color: var(--fg-2);
	}

	.banner-ok {
		--banner-border: var(--m-ok-bd);
		background: var(--m-ok-bg);
		border: 1px solid var(--banner-border);
	}
	.banner-ok :global(svg) {
		color: #289b4f;
	}

	.banner-warn {
		--banner-border: var(--m-warn-bd);
		background: var(--m-warn-bg);
		border: 1px solid var(--banner-border);
	}
	.banner-warn :global(svg) {
		color: #e0a800;
	}

	.banner-bad {
		--banner-border: var(--m-bad-bd);
		background: var(--m-bad-bg);
		border: 1px solid var(--banner-border);
	}
	.banner-bad :global(svg) {
		color: #df4730;
	}

	.banner-none {
		--banner-border: var(--border-default);
		background: var(--m-surface);
		border: 1px solid var(--banner-border);
	}
	.banner-none :global(svg) {
		color: var(--fg-2);
	}
</style>
