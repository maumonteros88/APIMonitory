<script lang="ts">
	import type { OutsideItem, OutsideStrip } from '$lib/filters';
	import { statusMeta } from '$lib/format';
	import Icon from './Icon.svelte';

	// Failures the environment/service filters hide, so a filter never hides
	// an outage silently. A chip brings its monitor into view next to what is
	// already shown; "Ver todos" drops both filters.
	let {
		strip,
		onReveal,
		onClearFilters
	}: {
		strip: OutsideStrip;
		onReveal: (item: OutsideItem) => void;
		onClearFilters: () => void;
	} = $props();
</script>

<div class="outside" role="status">
	<Icon name="error-warning-fill" />
	<span class="outside-text">{strip.text}</span>
	<span class="chips">
		{#each strip.items as item (item.id)}
			<button type="button" class="chip" title="Sumar a la vista" onclick={() => onReveal(item)}>
				<span class="dot" style="background: {statusMeta(item.status).dotColor}" aria-hidden="true"></span>
				<span>{item.label}{#if item.env}<span class="chip-env">{item.env}</span>{/if}</span>
			</button>
		{/each}
	</span>
	<button type="button" class="see-all" onclick={onClearFilters}>Ver todos</button>
</div>

<style>
	.outside {
		display: flex;
		align-items: center;
		gap: 12px;
		flex-wrap: wrap;
		padding: 12px 16px;
		border-radius: 8px;
		border: 1px dashed var(--m-bad-bd);
		background: var(--m-surface);
	}

	.outside > :global(svg) {
		flex-shrink: 0;
		color: var(--m-bad-ink);
	}

	.outside-text {
		font-size: 14px;
		font-weight: 700;
	}

	.chips {
		display: flex;
		gap: 6px;
		flex-wrap: wrap;
		min-width: 0;
	}

	.chip {
		display: flex;
		align-items: center;
		gap: 6px;
		padding: 2px 8px;
		border-radius: 4px;
		border: 1px solid var(--border-default);
		background: var(--m-soft);
		color: var(--fg-1);
		font-size: 12px;
		font-weight: 600;
		line-height: 1.5;
		text-align: left;
		cursor: pointer;
	}

	.chip:hover {
		border-color: var(--m-ink);
	}

	.dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		flex-shrink: 0;
	}

	.chip-env {
		font-weight: 400;
		color: var(--m-sub-tx);
	}

	.see-all {
		margin-left: auto;
		height: 32px;
		padding: 0 12px;
		border: 1px solid var(--border-default);
		border-radius: 4px;
		background: none;
		color: var(--fg-1);
		font-size: 12px;
		font-weight: 700;
		cursor: pointer;
	}

	.see-all:hover {
		background: var(--m-soft);
	}
</style>
