<script lang="ts">
	import { matchesServiceQuery, type ServiceFilterItem } from '$lib/filters';
	import type { EnvFilter } from '$lib/grouping';
	import EnvSwitch from './EnvSwitch.svelte';
	import Icon from './Icon.svelte';
	import ServiceCheckList from './ServiceCheckList.svelte';

	// The wide-screen filters: environment and services side by side with
	// the content. items are every service with its status in env; services
	// is the selection (empty = every service).
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

	let query = $state('');

	const shownItems = $derived(items.filter((item) => matchesServiceQuery(item, query)));
	const isAll = $derived(services.length === 0);
</script>

<section class="rail-section">
	<h2 id="{uid}-env" class="rail-heading">Ambiente</h2>
	<EnvSwitch {env} counts={envCounts} labelledBy="{uid}-env" fill onChange={onEnvChange} />
</section>

<section class="rail-section">
	<div class="rail-heading-row">
		<h2 id="{uid}-services" class="rail-heading">Servicios</h2>
		{#if !isAll}
			<button type="button" class="clear" onclick={onClearServices}>Limpiar ({services.length})</button>
		{/if}
	</div>
	<label class="search">
		<span class="visually-hidden">Buscar servicio</span>
		<Icon name="search-line" size={16} />
		<input type="search" placeholder="Buscar servicio" autocomplete="off" bind:value={query} />
	</label>
	<div class="service-list" role="group" aria-labelledby="{uid}-services">
		<button type="button" class="all-option" class:active={isAll} aria-pressed={isAll} onclick={onClearServices}>
			<span class="all-label">Todos los servicios</span>
			<span class="all-count">{envCounts[env]}</span>
		</button>
		<ServiceCheckList items={shownItems} onToggle={onToggleService} />
		{#if shownItems.length === 0 && query.trim() !== ''}
			<span class="no-match">Ningún servicio coincide.</span>
		{/if}
	</div>
</section>

<style>
	.rail-section {
		display: flex;
		flex-direction: column;
		gap: 10px;
	}

	.rail-heading-row {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 8px;
	}

	.rail-heading {
		margin: 0;
		font-size: 12px;
		line-height: 1.5;
		font-weight: 600;
		letter-spacing: 0.04em;
		text-transform: uppercase;
		color: var(--m-sub-tx);
	}

	.clear {
		padding: 0;
		border: 0;
		background: none;
		color: var(--m-sub-tx);
		font-size: 12px;
		font-weight: 600;
		text-decoration: underline;
		text-underline-offset: 3px;
		cursor: pointer;
	}

	.clear:hover {
		color: var(--fg-1);
	}

	.search {
		position: relative;
		display: block;
		color: var(--m-sub-tx);
	}

	.search :global(svg) {
		position: absolute;
		left: 12px;
		top: 12px;
		pointer-events: none;
	}

	.search input {
		width: 100%;
		height: 40px;
		box-sizing: border-box;
		padding: 0 12px 0 36px;
		border: 1px solid var(--border-default);
		border-radius: 4px;
		background: var(--m-surface);
		color: var(--fg-1);
		font-size: 14px;
	}

	.search input::placeholder {
		color: var(--m-sub-tx);
	}

	.search input:focus {
		outline: none;
		border: 2px solid var(--primary-500);
		padding-left: 35px;
		padding-right: 11px;
	}

	.service-list {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}

	.all-option {
		display: flex;
		align-items: center;
		gap: 10px;
		height: 40px;
		padding: 0 12px;
		border: 0;
		border-radius: 4px;
		background: transparent;
		color: var(--fg-1);
		font-size: 14px;
		font-weight: 600;
		text-align: left;
		cursor: pointer;
	}

	.all-option:hover,
	.all-option.active {
		background: var(--m-soft);
	}

	.all-option.active {
		font-weight: 700;
	}

	.all-label {
		flex: 1;
	}

	.all-count {
		font-size: 12px;
		font-weight: 600;
		color: var(--m-sub-tx);
	}

	.no-match {
		padding: 8px 12px;
		font-size: 12px;
		color: var(--m-sub-tx);
	}

	.visually-hidden {
		position: absolute;
		width: 1px;
		height: 1px;
		overflow: hidden;
		clip: rect(0 0 0 0);
		white-space: nowrap;
	}
</style>
