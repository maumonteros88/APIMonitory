<script lang="ts">
	import { ASSIGN_COMPANY_ATTR } from '$lib/companies';
	import Icon from './Icon.svelte';

	// The "Asignar empresa" icon button of a service's header (list, board and
	// matrix). The data attribute lets the dialog find the button again after
	// the service moved to another company section and the original element
	// was replaced.
	let { service, onAssign }: { service: string; onAssign: (service: string) => void } = $props();
</script>

<button
	type="button"
	class="assign-button"
	{...{ [ASSIGN_COMPANY_ATTR]: service }}
	aria-label="Asignar empresa a {service}"
	title="Asignar empresa a {service}"
	onclick={() => onAssign(service)}
>
	<Icon name="building-line" size={18} />
</button>

<style>
	.assign-button {
		width: 32px;
		height: 32px;
		display: flex;
		align-items: center;
		justify-content: center;
		flex-shrink: 0;
		padding: 0;
		border: 1px solid var(--border-default);
		border-radius: 4px;
		background: var(--m-surface);
		color: var(--m-ink);
		cursor: pointer;
	}

	.assign-button:hover {
		background: var(--m-soft);
	}
</style>
