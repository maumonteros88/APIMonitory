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
	import AddMonitorForm from '$lib/components/AddMonitorForm.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import MonitorCard from '$lib/components/MonitorCard.svelte';
	import StatusBanner from '$lib/components/StatusBanner.svelte';
	import ThemeToggle from '$lib/components/ThemeToggle.svelte';
	import TokenGate from '$lib/components/TokenGate.svelte';

	const POLL_MS = 15000;
	const THEME_KEY = 'apimonitory_theme';

	let authenticated = $state(getToken() !== null);
	let gateError = $state<string | null>(null);
	let gateSubmitting = $state(false);

	let monitors = $state<Monitor[]>([]);
	let intervalSeconds = $state(60);

	let formOpen = $state(false);
	let formError = $state<string | null>(null);
	let formSubmitting = $state(false);

	let theme = $state<'light' | 'dark'>('light');

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

	async function loadMonitors() {
		try {
			const res = await fetchMonitors(90);
			monitors = res.monitors;
			intervalSeconds = res.intervalSeconds;
		} catch (err) {
			if (!(err instanceof UnauthorizedError)) console.error('failed to load monitors', err);
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
	}

	async function handleCreate(input: NewMonitorInput) {
		formSubmitting = true;
		formError = null;
		try {
			const created = await createMonitor(input);
			monitors = [...monitors, created];
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

		<StatusBanner {monitors} {intervalSeconds} />

		{#if formOpen}
			<AddMonitorForm
				error={formError}
				submitting={formSubmitting}
				onCancel={() => (formOpen = false)}
				onSubmit={handleCreate}
			/>
		{/if}

		<section class="monitor-list-section">
			<div class="list-caption">Disponibilidad de los últimos 90 días</div>
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

	.actions button {
		display: inline-flex;
		align-items: center;
		gap: 6px;
	}

	.primary {
		background: var(--primary-500);
		color: #fff;
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
