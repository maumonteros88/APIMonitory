<script lang="ts">
	import { WORST_DOT, serviceItemMeta, type ServiceFilterItem } from '$lib/filters';

	// One real checkbox per service (shared by the rail and the compact
	// picker), so keyboard and screen-reader users get native toggling.
	let { items, onToggle }: { items: ServiceFilterItem[]; onToggle: (key: string) => void } = $props();
</script>

{#each items as item (item.key)}
	{@const meta = serviceItemMeta(item)}
	<label class="service-option" class:checked={item.checked} class:empty={item.count === 0}>
		<input type="checkbox" checked={item.checked} onchange={() => onToggle(item.key)} />
		<span class="dot" style="background: {WORST_DOT[item.worst]}" aria-hidden="true"></span>
		<span class="name">{item.name}</span>
		<span class="meta meta-{meta.tone}">{meta.text}</span>
	</label>
{/each}

<style>
	.service-option {
		display: flex;
		align-items: center;
		gap: 10px;
		min-height: 40px;
		padding: 0 12px;
		box-sizing: border-box;
		border-radius: 4px;
		font-size: 14px;
		font-weight: 600;
		color: var(--fg-1);
		cursor: pointer;
	}

	.service-option:hover {
		background: var(--m-soft);
	}

	.service-option.checked {
		background: var(--m-soft);
		font-weight: 700;
	}

	/* No monitor in the selected environment: still selectable, but quieter. */
	.service-option.empty .name {
		color: var(--m-sub-tx);
	}

	input {
		width: 16px;
		height: 16px;
		margin: 0;
		flex-shrink: 0;
		accent-color: var(--primary-500);
		cursor: pointer;
	}

	.dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		flex-shrink: 0;
	}

	.name {
		flex: 1;
		min-width: 0;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	.meta {
		font-size: 12px;
		font-weight: 600;
		white-space: nowrap;
	}

	.meta-plain {
		color: var(--m-sub-tx);
	}

	.meta-bad {
		color: var(--m-bad-ink);
	}

	.meta-warn {
		color: var(--m-warn-ink);
	}
</style>
