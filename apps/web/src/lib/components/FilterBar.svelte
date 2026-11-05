<script lang="ts">
	import type { ServiceFilterItem } from '$lib/filters';
	import type { EnvFilter } from '$lib/grouping';
	import EnvSwitch from './EnvSwitch.svelte';
	import ServicePicker from './ServicePicker.svelte';

	// The same filters as FilterRail, laid out in one wrapping row for
	// screens without room for the rail.
	let {
		env,
		envCounts,
		items,
		services,
		onEnvChange,
		onToggleService,
		onClearServices
	}: {
		env: EnvFilter;
		envCounts: Record<EnvFilter, number>;
		items: ServiceFilterItem[];
		services: string[];
		onEnvChange: (env: EnvFilter) => void;
		onToggleService: (key: string) => void;
		onClearServices: () => void;
	} = $props();

	const uid = $props.id();
</script>

<div class="filter-bar">
	<div class="env-filter">
		<span id="{uid}-env" class="filter-label">Ambiente</span>
		<EnvSwitch {env} counts={envCounts} labelledBy="{uid}-env" onChange={onEnvChange} />
	</div>
	<ServicePicker {items} {services} {onToggleService} {onClearServices} />
</div>

<style>
	.filter-bar {
		display: flex;
		align-items: center;
		gap: 12px 24px;
		flex-wrap: wrap;
	}

	.env-filter {
		display: flex;
		align-items: center;
		gap: 12px;
		flex-wrap: wrap;
		min-width: 0;
	}

	.filter-label {
		font-size: 14px;
		font-weight: 600;
		color: var(--m-sub-tx);
	}
</style>
