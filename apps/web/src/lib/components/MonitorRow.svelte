<script lang="ts">
	import { tick } from 'svelte';
	import type { Monitor } from '$lib/api';
	import { ago, statusMeta, uptimeLabel } from '$lib/format';
	import { ENV_LABELS } from '$lib/grouping';
	import Icon from './Icon.svelte';
	import UptimeBars from './UptimeBars.svelte';

	// stacked: the two-column card layout, with the status and uptime on the
	// name's line and the bars on their own full-width line below.
	let {
		monitor,
		showEnv,
		stacked = false,
		onCheck,
		onEdit,
		onDelete
	}: {
		monitor: Monitor;
		showEnv: boolean;
		stacked?: boolean;
		onCheck: (id: string) => void;
		onEdit: (monitor: Monitor) => void;
		onDelete: (id: string) => void;
	} = $props();

	const uid = $props.id();
	const tipId = `${uid}-tip`;
	const menuId = `${uid}-menu`;

	let tipOpen = $state(false);
	let menuOpen = $state(false);
	let menuWrap = $state<HTMLDivElement>();
	let trigger = $state<HTMLButtonElement>();
	let menu = $state<HTMLDivElement>();

	const meta = $derived(statusMeta(monitor.status));

	const lastInfo = $derived.by(() => {
		if (monitor.checking) return 'Verificando…';
		if (!monitor.lastCheck) return 'Sin verificar';
		const { httpStatus, latencyMs, at } = monitor.lastCheck;
		return `HTTP ${httpStatus} · ${latencyMs} ms · ${ago(at)}`;
	});

	const envBadge = $derived(monitor.environment ?? 'none');
	const envLabel = $derived(monitor.environment ? ENV_LABELS[monitor.environment] : 'Sin ambiente');

	function handleNameKeydown(event: KeyboardEvent) {
		if (event.key === 'Escape') tipOpen = false;
	}

	function menuItems(): HTMLButtonElement[] {
		return menu ? [...menu.querySelectorAll<HTMLButtonElement>('[role="menuitem"]')] : [];
	}

	async function openMenu() {
		menuOpen = true;
		await tick();
		menuItems()[0]?.focus();
	}

	function closeMenu(returnFocus: boolean) {
		menuOpen = false;
		if (returnFocus) trigger?.focus();
	}

	function handleMenuKeydown(event: KeyboardEvent) {
		const items = menuItems();
		const current = items.indexOf(document.activeElement as HTMLButtonElement);
		let next: number | null = null;
		if (event.key === 'ArrowDown') next = (current + 1) % items.length;
		else if (event.key === 'ArrowUp') next = (current - 1 + items.length) % items.length;
		else if (event.key === 'Home') next = 0;
		else if (event.key === 'End') next = items.length - 1;
		else if (event.key === 'Escape') {
			event.preventDefault();
			closeMenu(true);
		} else if (event.key === 'Tab') {
			closeMenu(false);
		}
		if (next !== null) {
			event.preventDefault();
			items[next]?.focus();
		}
	}

	// Closes the menu on any click outside it, only while it's open.
	$effect(() => {
		if (!menuOpen) return;
		function handlePointerDown(event: PointerEvent) {
			if (!menuWrap?.contains(event.target as Node)) menuOpen = false;
		}
		document.addEventListener('pointerdown', handlePointerDown);
		return () => document.removeEventListener('pointerdown', handlePointerDown);
	});

	function handleCheck() {
		closeMenu(true);
		onCheck(monitor.id);
	}

	function handleEdit() {
		closeMenu(false);
		onEdit(monitor);
	}

	function handleDelete() {
		closeMenu(true);
		if (confirm(`¿Eliminar ${monitor.name}?`)) {
			onDelete(monitor.id);
		}
	}
</script>

<div class="row" class:stacked>
	<div class="name-cell">
		<span class="status-dot" style="background: {meta.dotColor}" aria-hidden="true"></span>
		<button
			type="button"
			class="name"
			aria-describedby={tipId}
			onmouseenter={() => (tipOpen = true)}
			onmouseleave={() => (tipOpen = false)}
			onfocus={() => (tipOpen = true)}
			onblur={() => (tipOpen = false)}
			onkeydown={handleNameKeydown}
		>
			{monitor.name}
		</button>
		{#if showEnv}
			<span class="env-badge env-{envBadge}">{envLabel}</span>
		{/if}
		<div id={tipId} role="tooltip" class="tip" hidden={!tipOpen}>
			<span class="tip-status" style="color: {meta.textColor}">
				<span class="status-dot" style="background: {meta.dotColor}"></span>
				{meta.label}
			</span>
			<span class="tip-last">{lastInfo}</span>
			{#if monitor.description}<span class="tip-desc">{monitor.description}</span>{/if}
			<div class="tip-target">
				<span>{monitor.url}</span>
				{#if monitor.commit}<span class="tip-commit">commit {monitor.commit.slice(0, 7)}</span>{/if}
			</div>
		</div>
	</div>

	<div class="bars-cell">
		<UptimeBars history={monitor.history} monitorId={monitor.id} height={stacked ? 20 : 24} />
	</div>

	{#if stacked && monitor.status !== 'up'}
		<span class="status-text status-{monitor.status}">{meta.label}</span>
	{/if}

	<span class="uptime">{uptimeLabel(monitor.uptimePercent)}</span>

	<div class="menu-wrap" bind:this={menuWrap}>
		<button
			type="button"
			class="menu-trigger"
			aria-label="Acciones de {monitor.name}"
			title="Verificar ahora, editar o eliminar"
			aria-haspopup="menu"
			aria-expanded={menuOpen}
			aria-controls={menuOpen ? menuId : undefined}
			bind:this={trigger}
			onclick={() => (menuOpen ? closeMenu(false) : openMenu())}
		>
			<Icon name="more-2-fill" />
		</button>
		{#if menuOpen}
			<div
				id={menuId}
				class="menu"
				role="menu"
				aria-label="Acciones de {monitor.name}"
				tabindex="-1"
				bind:this={menu}
				onkeydown={handleMenuKeydown}
			>
				<button type="button" role="menuitem" tabindex="-1" onclick={handleCheck}>
					<Icon name="refresh-line" size={16} />
					Verificar ahora
				</button>
				<button type="button" role="menuitem" tabindex="-1" onclick={handleEdit}>
					<Icon name="edit-line" size={16} />
					Editar
				</button>
				<button type="button" role="menuitem" tabindex="-1" class="danger" onclick={handleDelete}>
					<Icon name="delete-bin-line" size={16} />
					Eliminar
				</button>
			</div>
		{/if}
	</div>
</div>

<style>
	.row {
		display: grid;
		grid-template-columns: 240px minmax(0, 1fr) 72px 32px;
		gap: 16px;
		align-items: center;
		padding: 14px 16px;
	}

	.name-cell {
		position: relative;
		display: flex;
		align-items: center;
		gap: 8px;
		min-width: 0;
	}

	.status-dot {
		width: 10px;
		height: 10px;
		border-radius: 50%;
		flex-shrink: 0;
	}

	.name {
		min-width: 0;
		padding: 0;
		border: 0;
		background: none;
		font-size: 16px;
		font-weight: 700;
		color: var(--fg-1);
		text-align: left;
		cursor: default;
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
		text-decoration: underline dotted var(--fg-3);
		text-underline-offset: 5px;
	}

	.env-badge {
		flex-shrink: 0;
		font-size: 12px;
		font-weight: 600;
		line-height: 1.5;
		padding: 0 6px;
		border-radius: 4px;
		border: 1px solid var(--border-default);
		background: var(--m-surface);
		color: var(--fg-2);
	}

	.env-prod {
		border-color: var(--m-ink);
		background: var(--m-ink);
		color: var(--m-surface);
	}

	.env-qa {
		border-color: var(--border-subtle);
		background: var(--border-subtle);
		color: var(--fg-1);
	}

	.env-prepro {
		border-color: var(--fg-2);
		color: var(--fg-1);
	}

	.env-dev {
		border-style: dashed;
		border-color: var(--fg-3);
	}

	.tip {
		position: absolute;
		top: calc(100% + 12px);
		left: 0;
		z-index: 10;
		width: 340px;
		max-width: calc(100vw - 48px);
		box-sizing: border-box;
		padding: 16px;
		background: var(--m-surface);
		border: 1px solid var(--border-default);
		border-radius: 8px;
		box-shadow: 0 12px 28px -6px var(--m-shadow);
		display: flex;
		flex-direction: column;
		gap: 8px;
	}

	.tip[hidden] {
		display: none;
	}

	.tip-status {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 14px;
		font-weight: 600;
	}

	.tip-last {
		font-size: 12px;
		color: var(--fg-2);
	}

	.tip-desc {
		font-size: 14px;
		color: var(--fg-2);
	}

	.tip-target {
		display: flex;
		flex-direction: column;
		gap: 4px;
		padding: 10px 12px;
		background: var(--m-soft);
		border-radius: 4px;
		font-size: 12px;
		font-family: var(--font-mono);
		color: var(--m-ink);
		word-break: break-all;
	}

	.tip-commit {
		color: var(--fg-2);
	}

	.bars-cell {
		min-width: 0;
	}

	.uptime {
		font-size: 14px;
		font-weight: 600;
		text-align: right;
		white-space: nowrap;
	}

	.menu-wrap {
		position: relative;
	}

	.menu-trigger {
		width: 32px;
		height: 32px;
		display: flex;
		align-items: center;
		justify-content: center;
		padding: 0;
		border: 0;
		border-radius: 4px;
		background: none;
		color: var(--fg-2);
		cursor: pointer;
	}

	.menu-trigger:hover,
	.menu-trigger[aria-expanded='true'] {
		background: var(--m-soft);
		color: var(--m-ink);
	}

	.menu {
		position: absolute;
		top: calc(100% + 4px);
		right: 0;
		z-index: 20;
		min-width: 180px;
		padding: 4px;
		display: flex;
		flex-direction: column;
		gap: 2px;
		background: var(--m-surface);
		border: 1px solid var(--border-default);
		border-radius: 8px;
		box-shadow: 0 12px 28px -6px var(--m-shadow);
	}

	.menu button {
		display: flex;
		align-items: center;
		gap: 8px;
		padding: 8px 12px;
		border: 0;
		border-radius: 4px;
		background: none;
		color: var(--m-ink);
		font-size: 14px;
		font-weight: 600;
		text-align: left;
		cursor: pointer;
	}

	.menu button:hover,
	.menu button:focus-visible {
		background: var(--m-soft);
		outline: none;
	}

	.menu .danger {
		color: var(--fg-2);
	}

	.menu .danger:hover,
	.menu .danger:focus-visible {
		color: var(--m-bad-tx);
	}

	.row.stacked {
		grid-template-columns: minmax(0, 1fr) auto auto 32px;
		grid-template-areas:
			'name status uptime menu'
			'bars bars bars bars';
		column-gap: 12px;
		row-gap: 8px;
		padding: 12px 12px 14px 16px;
	}

	.stacked .name-cell {
		grid-area: name;
	}

	.stacked .name {
		font-size: 15px;
	}

	.stacked .bars-cell {
		grid-area: bars;
	}

	.stacked .uptime {
		grid-area: uptime;
		min-width: 64px;
		font-weight: 700;
	}

	.stacked .menu-wrap {
		grid-area: menu;
	}

	.status-text {
		grid-area: status;
		font-size: 12px;
		font-weight: 600;
		white-space: nowrap;
	}

	.status-down {
		color: var(--m-bad-ink);
	}

	.status-degraded {
		color: var(--m-warn-ink);
	}

	.status-pending {
		color: var(--m-sub-tx);
	}

	/* Narrow screens: the bars drop to their own full-width line. */
	@media (max-width: 640px) {
		.row {
			grid-template-columns: minmax(0, 1fr) auto 32px;
			row-gap: 10px;
		}

		.bars-cell {
			grid-column: 1 / -1;
			grid-row: 2;
		}
	}
</style>
