<script lang="ts">
	import { PERIODS, type Period } from '$lib/period';

	let { value, onChange }: { value: Period; onChange: (days: Period) => void } = $props();

	const uid = $props.id();
</script>

<div class="period">
	<span id="{uid}-label" class="period-label">Disponibilidad de los últimos</span>
	<div class="period-switch" role="group" aria-labelledby="{uid}-label">
		{#each PERIODS as days (days)}
			<button
				type="button"
				class="period-option"
				class:active={value === days}
				aria-pressed={value === days}
				onclick={() => onChange(days)}
			>
				{days} días
			</button>
		{/each}
	</div>
</div>

<style>
	.period {
		display: flex;
		align-items: center;
		gap: 10px;
		flex-wrap: wrap;
	}

	.period-label {
		font-size: 14px;
		color: var(--m-sub-tx);
	}

	.period-switch {
		display: flex;
		height: 32px;
		box-sizing: border-box;
		padding: 2px;
		gap: 2px;
		border: 1px solid var(--border-default);
		border-radius: 4px;
		background: var(--m-surface);
	}

	.period-option {
		padding: 0 10px;
		border: 0;
		border-radius: 2px;
		background: transparent;
		color: var(--m-sub-tx);
		font-size: 12px;
		font-weight: 700;
		white-space: nowrap;
		cursor: pointer;
	}

	.period-option.active {
		background: var(--m-ink);
		color: var(--m-surface);
	}
</style>
