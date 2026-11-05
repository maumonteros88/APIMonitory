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

	/* Five options with their counts don't fit side by side in the 264px
	   rail or on a 320px phone, so there each count goes under its label:
	   the width then follows the labels and stays put as counts grow. */
	.fill {
		height: 48px;
	}

	.fill .env-option {
		flex: 1;
		flex-direction: column;
		padding: 0 4px;
		gap: 0;
		font-size: 13px;
		line-height: 1.2;
	}

	.fill .env-count {
		font-size: 11px;
	}

	@media (max-width: 480px) {
		.env-switch {
			height: 48px;
		}

		.env-option {
			flex: 1;
			flex-direction: column;
			padding: 0 6px;
			gap: 0;
			font-size: 13px;
			line-height: 1.2;
		}

		.env-count {
			font-size: 11px;
		}
	}
</style>
