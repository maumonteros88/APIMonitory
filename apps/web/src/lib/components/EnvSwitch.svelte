<script lang="ts">
	import { ENV_FILTERS, ENV_LABELS, type EnvFilter } from '$lib/grouping';

	// labelledBy points at the visible "Ambiente" label the caller renders, so
	// the same control fits the inline bar and the rail's section heading.
	// fill stretches the options across the rail's width.
	let {
		env,
		counts,
		labelledBy,
		fill = false,
		onChange
	}: {
		env: EnvFilter;
		counts: Record<EnvFilter, number>;
		labelledBy: string;
		fill?: boolean;
		onChange: (env: EnvFilter) => void;
	} = $props();
</script>

<div class="env-switch" class:fill role="group" aria-labelledby={labelledBy}>
	{#each ENV_FILTERS as option (option)}
		<button
			type="button"
			class="env-option"
			class:active={env === option}
			aria-pressed={env === option}
			onclick={() => onChange(option)}
		>
			{option === 'all' ? 'Todos' : ENV_LABELS[option]}
			<span class="env-count">{counts[option]}</span>
		</button>
	{/each}
</div>

<style>
	.env-switch {
		display: flex;
		height: 40px;
		padding: 3px;
		box-sizing: border-box;
		border: 1px solid var(--border-default);
		border-radius: 4px;
		background: var(--m-surface);
		gap: 2px;
	}

	.env-option {
		border: 0;
		border-radius: 2px;
		padding: 0 14px;
		font-size: 14px;
		font-weight: 700;
		cursor: pointer;
		display: flex;
		align-items: center;
		justify-content: center;
		gap: 6px;
		background: transparent;
		color: var(--m-sub-tx);
		white-space: nowrap;
	}

	.env-option.active {
		background: var(--m-ink);
		color: var(--m-surface);
	}

	.env-count {
		font-size: 12px;
		font-weight: 400;
	}

	.fill .env-option {
		flex: 1;
		padding: 0;
		gap: 4px;
		font-size: 13px;
	}

	.fill .env-count {
		font-size: 11px;
	}

	/* Keeps the four options within a 320px phone without page scroll. */
	@media (max-width: 400px) {
		.env-option {
			padding: 0 8px;
		}
	}
</style>
