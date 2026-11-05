<script lang="ts">
	import { tick } from 'svelte';
	import { WORST_DOT, pickerLabel, serviceLabel, type ServiceFilterItem } from '$lib/filters';
	import Icon from './Icon.svelte';
	import ServiceCheckList from './ServiceCheckList.svelte';

	// The services filter when there is no room for the rail: a button that
	// opens the checkbox list, plus one removable chip per selected service.
	let {
		items,
		grouped,
		services,
		onToggleService,
		onToggleCompany,
		onClearServices
	}: {
		items: ServiceFilterItem[];
		grouped: boolean;
		services: string[];
		onToggleService: (key: string) => void;
		onToggleCompany: (serviceKeys: string[]) => void;
		onClearServices: () => void;
	} = $props();

	const uid = $props.id();
	const popoverId = `${uid}-popover`;

	let open = $state(false);
	let anchor = $state<HTMLDivElement>();
	let trigger = $state<HTMLButtonElement>();
	let popover = $state<HTMLDivElement>();

	const chips = $derived(
		services.map((key) => ({
			key,
			name: serviceLabel(key),
			worst: items.find((item) => item.key === key)?.worst ?? 'none'
		}))
	);

	async function openPopover() {
		open = true;
		await tick();
		popover?.querySelector<HTMLInputElement>('input[type="checkbox"]')?.focus();
	}

	function closePopover(returnFocus: boolean) {
		open = false;
		if (returnFocus) trigger?.focus();
	}

	// The chip (and its button) disappears, so focus would fall back to the
	// page; the picker button is the closest place to continue from.
	async function removeChip(key: string) {
		onToggleService(key);
		await tick();
		trigger?.focus();
	}

	function handleKeydown(event: KeyboardEvent) {
		if (event.key === 'Escape' && open) {
			event.preventDefault();
			closePopover(true);
		}
	}

	// Leaving with Tab closes it like a click outside would; focus moving to
	// nowhere (relatedTarget null, e.g. a click on the popover's padding)
	// must not.
	function handleFocusOut(event: FocusEvent) {
		const next = event.relatedTarget as Node | null;
		if (open && next && !anchor?.contains(next)) open = false;
	}

	$effect(() => {
		if (!open) return;
		function handlePointerDown(event: PointerEvent) {
			if (!anchor?.contains(event.target as Node)) open = false;
		}
		document.addEventListener('pointerdown', handlePointerDown);
		return () => document.removeEventListener('pointerdown', handlePointerDown);
	});
</script>

<div class="picker">
	<!-- svelte-ignore a11y_no_static_element_interactions -->
	<div class="anchor" bind:this={anchor} onkeydown={handleKeydown} onfocusout={handleFocusOut}>
		<span id="{uid}-label" class="picker-label">Servicios</span>
		<button
			type="button"
			class="trigger"
			aria-labelledby="{uid}-label {uid}-value"
			aria-expanded={open}
			aria-controls={open ? popoverId : undefined}
			bind:this={trigger}
			onclick={() => (open ? closePopover(false) : openPopover())}
		>
			<span id="{uid}-value">{pickerLabel(services)}</span>
			<Icon name="arrow-down-s-line" size={16} />
		</button>
		{#if open}
			<div id={popoverId} class="popover" role="group" aria-label="Elegir servicios" bind:this={popover}>
				<div class="popover-list">
					<ServiceCheckList {items} {grouped} onToggle={onToggleService} {onToggleCompany} />
				</div>
				<div class="popover-actions">
					<button type="button" class="see-all" onclick={onClearServices}>Ver todos</button>
					<button type="button" class="done" onclick={() => closePopover(true)}>Listo</button>
				</div>
			</div>
		{/if}
	</div>

	{#each chips as chip (chip.key)}
		<span class="chip">
			<span class="dot" style="background: {WORST_DOT[chip.worst]}" aria-hidden="true"></span>
			{chip.name}
			<button
				type="button"
				class="chip-remove"
				aria-label="Quitar {chip.name}"
				title="Quitar {chip.name}"
				onclick={() => removeChip(chip.key)}
			>
				<Icon name="close-line" size={14} />
			</button>
		</span>
	{/each}
</div>

<style>
	.picker {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
		min-width: 0;
	}

	.anchor {
		position: relative;
		display: flex;
		align-items: center;
		gap: 12px;
	}

	.picker-label {
		font-size: 14px;
		font-weight: 600;
		color: var(--m-sub-tx);
	}

	.trigger {
		height: 40px;
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 0 12px;
		border: 1px solid var(--border-default);
		border-radius: 4px;
		background: var(--m-surface);
		color: var(--fg-1);
		font-size: 14px;
		font-weight: 700;
		cursor: pointer;
	}

	.trigger :global(svg) {
		color: var(--m-sub-tx);
	}

	.trigger:hover,
	.trigger[aria-expanded='true'] {
		background: var(--m-soft);
	}

	/* Anchored to the label's left edge, not the button's, so on a phone the
	   panel starts at the page gutter and never pushes the page sideways. */
	.popover {
		position: absolute;
		top: calc(100% + 8px);
		left: 0;
		z-index: 20;
		width: 320px;
		max-width: calc(100vw - 48px);
		box-sizing: border-box;
		padding: 8px;
		display: flex;
		flex-direction: column;
		background: var(--m-surface);
		border: 1px solid var(--border-default);
		border-radius: 8px;
		box-shadow: 0 12px 28px -6px var(--m-shadow);
	}

	.popover-list {
		display: flex;
		flex-direction: column;
		gap: 2px;
		max-height: min(360px, 60vh);
		overflow-y: auto;
	}

	.popover-actions {
		display: flex;
		justify-content: space-between;
		align-items: center;
		margin-top: 6px;
		padding: 8px 4px 0;
		border-top: 1px solid var(--border-subtle);
	}

	.see-all,
	.done {
		height: 32px;
		border: 0;
		border-radius: 4px;
		font-size: 12px;
		font-weight: 700;
		cursor: pointer;
	}

	.see-all {
		padding: 0 8px;
		background: none;
		color: var(--m-sub-tx);
	}

	.see-all:hover {
		color: var(--fg-1);
	}

	.done {
		padding: 0 12px;
		background: var(--m-ink);
		color: var(--m-surface);
	}

	.chip {
		display: flex;
		align-items: center;
		gap: 6px;
		height: 32px;
		box-sizing: border-box;
		max-width: 100%;
		padding: 0 2px 0 10px;
		border-radius: 4px;
		background: var(--m-soft);
		border: 1px solid var(--border-default);
		font-size: 12px;
		font-weight: 600;
		color: var(--fg-1);
	}

	.dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		flex-shrink: 0;
	}

	.chip-remove {
		width: 28px;
		height: 28px;
		display: flex;
		align-items: center;
		justify-content: center;
		padding: 0;
		border: 0;
		border-radius: 2px;
		background: none;
		color: var(--m-sub-tx);
		cursor: pointer;
	}

	.chip-remove:hover {
		background: var(--border-default);
		color: var(--fg-1);
	}
</style>
