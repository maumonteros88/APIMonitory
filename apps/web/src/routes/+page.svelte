<script lang="ts">
	import { onMount } from 'svelte';
	import {
		UnauthorizedError,
		checkAllMonitors,
		checkMonitor,
		createMonitor,
		deleteMonitor,
		fetchMonitors,
		onUnauthorized,
		probeToken,
		type Monitor,
		type NewMonitorInput
	} from '$lib/api';
	import { getToken, setToken } from '$lib/auth';
	import { ago } from '$lib/format';
	import AddMonitorForm from '$lib/components/AddMonitorForm.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import MonitorBoard from '$lib/components/MonitorBoard.svelte';
	import MonitorCard from '$lib/components/MonitorCard.svelte';
	import StatusBanner from '$lib/components/StatusBanner.svelte';
	import ThemeToggle from '$lib/components/ThemeToggle.svelte';
	import TokenGate from '$lib/components/TokenGate.svelte';

	const POLL_MS = 15000;
	const THEME_KEY = 'apimonitory_theme';
	const VIEW_KEY = 'apimonitory_view';
	const DAYS_CAPTION = 'Disponibilidad de los últimos 90 días';

	let authenticated = $state(getToken() !== null);
	let gateError = $state<string | null>(null);
	let gateSubmitting = $state(false);

	let monitors = $state<Monitor[]>([]);
	let intervalSeconds = $state(60);
	let hasLoadedOnce = $state(false);
	let lastSuccessAt = $state<number | null>(null);
	let lastFailureAt = $state<number | null>(null);

	// A poll failure only matters if it's the most recent attempt: a
	// success after a failure clears it without needing to reset
	// lastFailureAt itself.
	const connectionError = $derived(
		lastFailureAt !== null && (lastSuccessAt === null || lastFailureAt > lastSuccessAt)
	);

	const connectionSub = $derived.by(() => {
		// Read lastFailureAt so this recomputes on every failed poll tick
		// (every POLL_MS), keeping the "hace X" wording fresh even though
		// lastSuccessAt itself doesn't change while the outage continues.
		void lastFailureAt;
		if (!hasLoadedOnce || lastSuccessAt === null) return 'No se pudo conectar con el servidor';
		return `Mostrando datos de ${ago(new Date(lastSuccessAt).toISOString())}`;
	});

	let formOpen = $state(false);
	let formError = $state<string | null>(null);
	let formSubmitting = $state(false);

	let theme = $state<'light' | 'dark'>('light');
	let view = $state<'list' | 'board'>('list');

	let pollHandle: ReturnType<typeof setInterval> | undefined;

	function applyTheme(next: 'light' | 'dark') {
		theme = next;
		document.documentElement.setAttribute('data-theme', next);
		try {
			localStorage.setItem(THEME_KEY, next);
		} catch {
			// Storage unavailable: the toggle still works for this tab.
		}
	}

	function toggleTheme() {
		applyTheme(theme === 'dark' ? 'light' : 'dark');
	}

	function setView(next: 'list' | 'board') {
		view = next;
		try {
			localStorage.setItem(VIEW_KEY, next);
		} catch {
			// Storage unavailable: the toggle still works for this tab.
		}
	}

	async function loadMonitors() {
		try {
			const res = await fetchMonitors(90);
			monitors = res.monitors;
			intervalSeconds = res.intervalSeconds;
			hasLoadedOnce = true;
			lastSuccessAt = Date.now();
		} catch (err) {
			if (err instanceof UnauthorizedError) return;
			console.error('failed to load monitors', err);
			lastFailureAt = Date.now();
		}
	}

	function startPolling() {
		stopPolling();
		pollHandle = setInterval(loadMonitors, POLL_MS);
	}

	function stopPolling() {
		if (pollHandle) clearInterval(pollHandle);
		pollHandle = undefined;
	}

	async function handleGateSubmit(token: string) {
		gateSubmitting = true;
		gateError = null;
		try {
			await probeToken(token);
			setToken(token);
			authenticated = true;
			await loadMonitors();
			startPolling();
		} catch (err) {
			gateError = err instanceof UnauthorizedError ? 'Token inválido' : 'No se pudo conectar con el servidor';
		} finally {
			gateSubmitting = false;
		}
	}

	function handleUnauthorized() {
		stopPolling();
		authenticated = false;
		monitors = [];
		hasLoadedOnce = false;
		lastSuccessAt = null;
		lastFailureAt = null;
	}

	async function handleCreate(input: NewMonitorInput) {
		formSubmitting = true;
		formError = null;
		try {
			const created = await createMonitor(input);
			// Never blindly append: a concurrent poll that resolves around the
			// same time can already include this monitor (the server created it
			// before either request finished), and appending it again would
			// leave two entries with the same id, crashing the keyed each-block.
			monitors = monitors.some((m) => m.id === created.id)
				? monitors.map((m) => (m.id === created.id ? created : m))
				: [...monitors, created];
			formOpen = false;
		} catch (err) {
			formError = err instanceof Error ? err.message : 'Ocurrió un error inesperado';
		} finally {
			formSubmitting = false;
		}
	}

	async function handleCheck(id: string) {
		monitors = monitors.map((m) => (m.id === id ? { ...m, checking: true } : m));
		try {
			const updated = await checkMonitor(id);
			monitors = monitors.map((m) => (m.id === id ? updated : m));
		} catch (err) {
			if (!(err instanceof UnauthorizedError)) console.error('check failed', err);
			monitors = monitors.map((m) => (m.id === id ? { ...m, checking: false } : m));
		}
	}

	async function handleDelete(id: string) {
		try {
			await deleteMonitor(id);
			monitors = monitors.filter((m) => m.id !== id);
		} catch (err) {
			if (!(err instanceof UnauthorizedError)) console.error('delete failed', err);
		}
	}

	async function handleCheckAll() {
		try {
			await checkAllMonitors();
		} catch (err) {
			if (!(err instanceof UnauthorizedError)) console.error('check all failed', err);
		}
	}

	onMount(() => {
		theme = document.documentElement.getAttribute('data-theme') === 'dark' ? 'dark' : 'light';
		try {
			const savedView = localStorage.getItem(VIEW_KEY);
			if (savedView === 'board' || savedView === 'list') view = savedView;
		} catch {
			// Storage unavailable: defaults to list view for this tab.
		}
		const unsubscribe = onUnauthorized(handleUnauthorized);

		if (authenticated) {
			loadMonitors();
			startPolling();
		}

		return () => {
			unsubscribe();
			stopPolling();
		};
	});
</script>

<svelte:head>
	<title>Estado de APIs · GOIAR</title>
</svelte:head>

{#if !authenticated}
	<TokenGate error={gateError} submitting={gateSubmitting} onSubmit={handleGateSubmit} />
{:else}
	<div class="page">
		<header class="page-header">
			<div class="titles">
				<span class="eyebrow">GOIAR</span>
				<h1>Estado de APIs</h1>
			</div>
			<div class="actions">
				<div class="view-switch">
					<button
						type="button"
						class="view-option"
						class:active={view === 'list'}
						onclick={() => setView('list')}
					>
						<Icon name="list-check" />
						Lista
					</button>
					<button
						type="button"
						class="view-option"
						class:active={view === 'board'}
						onclick={() => setView('board')}
					>
						<Icon name="layout-masonry-line" />
						Board
					</button>
				</div>
				<ThemeToggle {theme} onToggle={toggleTheme} />
				<button type="button" onclick={handleCheckAll}>
					<Icon name="refresh-line" />
					Verificar todas
				</button>
				<button type="button" class="primary" onclick={() => (formOpen = !formOpen)}>
					<Icon name="add-line" />
					Agregar API
				</button>
			</div>
		</header>

		{#if connectionError}
			<div class="connection-banner">
				<Icon name="error-warning-line" />
				<div class="connection-copy">
					<span class="connection-text">No se pudo actualizar el estado</span>
					<span class="connection-sub">{connectionSub}</span>
				</div>
			</div>
		{/if}

		{#if hasLoadedOnce}
			<StatusBanner {monitors} {intervalSeconds} />
		{/if}

		{#if formOpen}
			<AddMonitorForm
				error={formError}
				submitting={formSubmitting}
				onCancel={() => (formOpen = false)}
				onSubmit={handleCreate}
			/>
		{/if}

		{#if !hasLoadedOnce && connectionError}
			<section class="monitor-list-section">
				<div class="list-caption">{DAYS_CAPTION}</div>
				<div class="monitor-list">
					<div class="empty-state">
						<Icon name="pulse-line" size={32} />
						<p class="empty-title">No se pudo conectar con el servidor</p>
						<p class="empty-sub">Reintentando automáticamente…</p>
					</div>
				</div>
			</section>
		{:else if hasLoadedOnce && view === 'board'}
			<MonitorBoard {monitors} daysCaption={DAYS_CAPTION} onCheck={handleCheck} />
		{:else if hasLoadedOnce}
			<section class="monitor-list-section">
				<div class="list-caption">{DAYS_CAPTION}</div>
				<div class="monitor-list">
					{#if monitors.length === 0}
						<div class="empty-state">
							<Icon name="pulse-line" size={32} />
							<p class="empty-title">No hay APIs para monitorear</p>
							<p class="empty-sub">
								Agrega la URL de health de una API para empezar a ver su estado.
							</p>
						</div>
					{:else}
						{#each monitors as monitor (monitor.id)}
							<MonitorCard {monitor} onCheck={handleCheck} onDelete={handleDelete} />
						{/each}
					{/if}
				</div>
			</section>
		{/if}
	</div>
{/if}

<style>
	.page {
		max-width: 960px;
		margin: 0 auto;
		padding: 48px 24px 80px;
		display: flex;
		flex-direction: column;
		gap: 32px;
	}

	.page-header {
		display: flex;
		align-items: flex-end;
		justify-content: space-between;
		gap: 16px;
		flex-wrap: wrap;
	}

	.titles {
		display: flex;
		flex-direction: column;
		gap: 4px;
	}

	.eyebrow {
		font-size: 12px;
		font-weight: 600;
		color: var(--fg-2);
		letter-spacing: 0.04em;
	}

	.titles h1 {
		margin: 0;
		font-size: 36px;
		line-height: 1.2;
		font-weight: 700;
		letter-spacing: -0.01em;
	}

	.actions {
		display: flex;
		gap: 8px;
		align-items: center;
	}

	/* Direct child only, so the segmented view-switch's own buttons (nested
	   one level deeper) don't inherit this full-size outlined-button look. */
	.actions > button {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		height: 40px;
		padding: 0 16px;
		border: 1px solid var(--m-ink);
		border-radius: 4px;
		background: var(--m-surface);
		color: var(--m-ink);
		font-size: 14px;
		font-weight: 700;
		cursor: pointer;
	}

	.actions > button:hover {
		background: var(--m-soft);
	}

	.primary {
		border: 0;
		background: var(--primary-500);
		color: #fff;
	}

	.primary:hover {
		background: var(--primary-700);
	}

	.view-switch {
		display: flex;
		height: 40px;
		padding: 3px;
		box-sizing: border-box;
		border: 1px solid var(--border-default);
		border-radius: 4px;
		background: var(--m-surface);
		gap: 2px;
	}

	.view-option {
		border: 0;
		border-radius: 2px;
		padding: 0 12px;
		font-size: 14px;
		font-weight: 700;
		cursor: pointer;
		display: flex;
		align-items: center;
		gap: 6px;
		background: transparent;
		color: var(--fg-2);
	}

	.view-option.active {
		background: var(--m-ink);
		color: var(--m-surface);
	}

	.connection-banner {
		display: flex;
		align-items: center;
		gap: 12px;
		padding: 12px 20px;
		border-radius: 8px;
		background: var(--m-warn-bg);
		border: 1px solid var(--m-warn-bd);
	}

	.connection-banner :global(svg) {
		color: #e0a800;
		flex-shrink: 0;
	}

	.connection-copy {
		display: flex;
		flex-direction: column;
		gap: 2px;
	}

	.connection-text {
		font-size: 14px;
		font-weight: 700;
	}

	.connection-sub {
		font-size: 12px;
		color: var(--fg-2);
	}

	.list-caption {
		text-align: right;
		font-size: 14px;
		color: var(--fg-2);
		margin-bottom: 12px;
	}

	.monitor-list {
		background: var(--m-surface);
		border: 1px solid var(--border-default);
		border-radius: 8px;
	}

	.monitor-list :global(article + article) {
		border-top: 1px solid var(--border-subtle);
	}

	.empty-state {
		padding: 48px 24px;
		text-align: center;
		display: flex;
		flex-direction: column;
		align-items: center;
		gap: 8px;
		color: var(--fg-2);
	}

	.empty-title {
		margin: 0;
		font-weight: 600;
		color: var(--fg-1);
	}

	.empty-sub {
		margin: 0;
	}
</style>
