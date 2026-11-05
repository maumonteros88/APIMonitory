<script lang="ts">
	import { companySelection } from '$lib/companies';
	import { WORST_DOT, groupFilterItems, serviceItemMeta, type ServiceFilterItem } from '$lib/filters';

	// One real checkbox per service (shared by the rail and the compact
	// picker), so keyboard and screen-reader users get native toggling. With
	// grouped, the services sit under company sub-rows: each one a checkbox
	// that is checked, unchecked or indeterminate as all, none or some of its
	// services are selected, and that onToggleCompany applies to those listed
	// services. The selection itself still holds services only.
	let {
		items,
		grouped = false,
		onToggle,
		onToggleCompany
	}: {
		items: ServiceFilterItem[];
		grouped?: boolean;
		onToggle: (key: string) => void;
		onToggleCompany: (serviceKeys: string[]) => void;
	} = $props();

	const layout = $derived(grouped ? groupFilterItems(items) : null);
</script>

{#snippet option(item: ServiceFilterItem)}
	{@const meta = serviceItemMeta(item)}
	<label class="service-option" class:checked={item.checked} class:empty={item.count === 0}>
		<input type="checkbox" checked={item.checked} onchange={() => onToggle(item.key)} />
		<span class="dot" style="background: {WORST_DOT[item.worst]}" aria-hidden="true"></span>
		<span class="name">{item.name}</span>
		<span class="meta meta-{meta.tone}">{meta.text}</span>
	</label>
{/snippet}

{#if layout}
	{#each layout.blocks as block (block.company ?? '')}
		{@const keys = block.items.map((item) => item.key)}
		{@const selection = companySelection(
			block.items.filter((item) => item.checked).map((item) => item.key),
			keys
		)}
		<div class="company-block">
			<label class="company-option">
				<input
					type="checkbox"
					checked={selection === 'all'}
					{@attach (node) => {
						node.indeterminate = selection === 'some';
					}}
					onchange={() => onToggleCompany(keys)}
				/>
				<span class="name">{block.company ?? 'Sin empresa'}</span>
			</label>
			<div class="company-services">
				{#each block.items as item (item.key)}
					{@render option(item)}
				{/each}
			</div>
		</div>
	{/each}
	{#each layout.noService as item (item.key)}
		{@render option(item)}
	{/each}
{:else}
	{#each items as item (item.key)}
		{@render option(item)}
	{/each}
{/if}

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

	.company-block {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}

	.company-option {
		display: flex;
		align-items: center;
		gap: 10px;
		min-height: 36px;
		padding: 0 12px;
		box-sizing: border-box;
		border-radius: 4px;
		font-size: 12px;
		font-weight: 700;
		letter-spacing: 0.04em;
		text-transform: uppercase;
		color: var(--m-sub-tx);
		cursor: pointer;
	}

	.company-option:hover {
		background: var(--m-soft);
		color: var(--fg-1);
	}

	.company-services {
		display: flex;
		flex-direction: column;
		gap: 2px;
		padding-left: 16px;
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
