<script lang="ts">
	import { onMount } from 'svelte';
	import { MediaQuery } from 'svelte/reactivity';
	import {
		PUBLIC_API_VERSION,
		PUBLIC_GIT_COMMIT,
		PUBLIC_WEB_VERSION
	} from '$env/static/public';
	import {
		UnauthorizedError,
		applyMonitorDelta,
		checkAllMonitors,
		checkMonitor,
		createMonitor,
		deleteMonitor,
		fetchMonitors,
		mergeMonitors,
		mergeRecentMonitors,
		onUnauthorized,
		probeToken,
		subscribeToMonitorEvents,
		updateMonitor,
		upsertMonitor,
		type Environment,
		type Monitor,
		type MonitorKind,
		type NewMonitorInput
	} from '$lib/api';
	import { getToken, setToken } from '$lib/auth';
	import { ago } from '$lib/format';
	import {
		emptyStateCopy,
		filterMonitors,
		parseServices,
		pruneServices,
		revealMonitor,
		scopeTitle,
		serviceFilterItems,
		toggleService,
		type OutsideItem
	} from '$lib/filters';
	import { envCounts, isEnvFilter, serviceNames, type EnvFilter } from '$lib/grouping';
	import { DEFAULT_PERIOD, applyPeriod, parsePeriod, type Period } from '$lib/period';
	import { VIEW_OPTIONS, isView, type View } from '$lib/views';
	import AddMonitorForm from '$lib/components/AddMonitorForm.svelte';
	import ChartView from '$lib/components/ChartView.svelte';
	import FilterBar from '$lib/components/FilterBar.svelte';
	import FilterRail from '$lib/components/FilterRail.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import MonitorBoard from '$lib/components/MonitorBoard.svelte';
	import MonitorList from '$lib/components/MonitorList.svelte';
	import StatusBanner from '$lib/components/StatusBanner.svelte';
	import ThemeToggle from '$lib/components/ThemeToggle.svelte';
	import TokenGate from '$lib/components/TokenGate.svelte';

	// The poll is now just a reconciliation safety net, and as of the
	// recent-history change it only fetches each monitor's last couple of
	// days (see pollRecent) instead of the full 90 -- self-healing for
	// anything older than that comes from the full resync on SSE reconnect
	// (the streamDropped latch in startEvents) and from needsResync
	// escalating any detected gap back to loadMonitors(). Real-time updates
	// come from the SSE stream (see startEvents), which drops a subscriber
	// that can't keep up rather than losing events silently, and the events
	// connection is closed and fully reloaded on tab visibility changes --
	// so a slow, infrequent backstop is enough here.
	const POLL_MS = 60000;
	// Keeps "hace X" text (the connection banner) from looking frozen now
	// that the network poll driving it is so much slower.
	const NOW_TICK_MS = 15000;
	const THEME_KEY = 'apimonitory_theme';
	const VIEW_KEY = 'apimonitory_view';
	const ENV_KEY = 'apimonitory_env';
	const SERVICES_KEY = 'apimonitory_services';
	const DAYS_KEY = 'apimonitory_days';
	const BOARD_PREFILL_NOTE = 'Servicio, tipo y ambiente vienen de la tarjeta. Solo falta el nombre y la URL.';

	// Viewport, not container, breakpoints. The rail is a page-level layout
	// choice. The two-column list needs 2 × 600px cards + 16px gap = 1216px
	// of content: at 1600px that is 1600 − 80 page padding − 264 rail − 32
	// gap = 1224px. A container query would measure that column directly,
	// but a query container contains layout and becomes the containing block
	// of the bars' position: fixed day tooltip, misplacing it.
	const wideLayout = new MediaQuery('(min-width: 1440px)');
	const twoColumnList = new MediaQuery('(min-width: 1600px)');

	let authenticated = $state(getToken() !== null);
	let gateError = $state<string | null>(null);
	let gateSubmitting = $state(false);

	let monitors = $state<Monitor[]>([]);
	let intervalSeconds = $state(60);
	let hasLoadedOnce = $state(false);
	let lastSuccessAt = $state<number | null>(null);
	let lastFailureAt = $state<number | null>(null);
	// Ticks independently of any network activity (poll or SSE), purely so
	// connectionSub's "hace X" wording doesn't look frozen between polls.
	let now = $state(Date.now());
	// The chart view refreshes its own data only while this is true.
	let tabVisible = $state(true);

	// A poll failure only matters if it's the most recent attempt: a
	// success after a failure clears it without needing to reset
	// lastFailureAt itself.
	const connectionError = $derived(
		lastFailureAt !== null && (lastSuccessAt === null || lastFailureAt > lastSuccessAt)
	);

	const connectionSub = $derived.by(() => {
		// Read now/lastFailureAt so this recomputes on every tick of either,
		// keeping the "hace X" wording fresh even though lastSuccessAt
		// itself doesn't change while the outage continues.
		void now;
		void lastFailureAt;
		if (!hasLoadedOnce || lastSuccessAt === null) return 'No se pudo conectar con el servidor';
		return `Mostrando datos de ${ago(new Date(lastSuccessAt).toISOString())}`;
	});

	let formOpen = $state(false);
	let formError = $state<string | null>(null);
	let formSubmitting = $state(false);
	let editingMonitor = $state<Monitor | null>(null);

	interface FormPrefill {
		service: string | null;
		kind: MonitorKind | null;
		environment: Environment | null;
		note: string | null;
	}

	let formPrefill = $state<FormPrefill>({ service: null, kind: null, environment: null, note: null });
	// A new prefill (another "+" clicked while the form is open) must
	// re-create the form, whose fields only read their initial values.
	const formKey = $derived(
		editingMonitor
			? editingMonitor.id
			: `new|${formPrefill.service ?? ''}|${formPrefill.kind ?? ''}|${formPrefill.environment ?? ''}`
	);

	let theme = $state<'light' | 'dark'>('light');
	let view = $state<View>('list');
	let env = $state<EnvFilter>('all');
	// Service names, '' for "Sin servicio"; empty means every service.
	let services = $state<string[]>([]);
	let days = $state<Period>(DEFAULT_PERIOD);

	// Everything below the filters (banners, every view) only sees the
	// monitors in the selected environment and services, with their full
	// 90-day history; each view applies the period on top.
	const visibleMonitors = $derived(filterMonitors(monitors, env, services));
	const periodMonitors = $derived(applyPeriod(visibleMonitors, days));
	const envMonitorCounts = $derived(envCounts(monitors));
	const serviceItems = $derived(serviceFilterItems(monitors, env, services));
	const knownServices = $derived(serviceNames(monitors));
	const emptyCopy = $derived(emptyStateCopy(monitors.length, env, services));

	// Only against a loaded list: before the first load monitors is [] and
	// pruning would wipe the saved selection on every page load. A service
	// with no monitor in the current environment still exists and stays.
	$effect(() => {
		if (!hasLoadedOnce) return;
		const pruned = pruneServices(services, monitors);
		if (pruned !== services) setServices(pruned);
	});

	let pollHandle: ReturnType<typeof setInterval> | undefined;
	let nowTickHandle: ReturnType<typeof setInterval> | undefined;
	let eventsClose: (() => void) | null = null;
	let hiddenTeardownHandle: ReturnType<typeof setTimeout> | undefined;
	// Shared across every caller that wants a full 90-day resync (initial
	// load, token gate, visibility recovery, a needsResync escalation from
	// the recent poll or an SSE delta) so concurrent callers around the same
	// moment share one request instead of firing one each.
	let fullSyncInFlight: Promise<void> | null = null;
	// Set on the first onConnectionChange(false) after a connection is
	// (re)established, cleared on the first onConnectionChange(true) that
	// follows -- so exactly that first reconnect signal (not every 25s
	// heartbeat) triggers a full resync, covering whatever the client missed
	// while the stream was down.
	let streamDropped = false;

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

	function setView(next: View) {
		view = next;
		try {
			localStorage.setItem(VIEW_KEY, next);
		} catch {
			// Storage unavailable: the toggle still works for this tab.
		}
	}

	function setEnv(next: EnvFilter) {
		env = next;
		try {
			localStorage.setItem(ENV_KEY, next);
		} catch {
			// Storage unavailable: the filter still works for this tab.
		}
	}

	function setServices(next: string[]) {
		services = next;
		try {
			localStorage.setItem(SERVICES_KEY, JSON.stringify(next));
		} catch {
			// Storage unavailable: the filter still works for this tab.
		}
	}

	function setDays(next: Period) {
		days = next;
		try {
			localStorage.setItem(DAYS_KEY, String(next));
		} catch {
			// Storage unavailable: the period still works for this tab.
		}
	}

	function handleToggleService(key: string) {
		setServices(toggleService(services, key));
	}

	function clearServices() {
		setServices([]);
	}

	function clearFilters() {
		setEnv('all');
		setServices([]);
	}

	// A chip in a regular alert: its monitor is already visible, so the view
	// narrows to that service alone.
	function focusService(service: string | null) {
		setServices([service ?? '']);
	}

	// A chip in the "fuera de este filtro" strip: its monitor joins the view
	// instead of replacing what is shown.
	function revealOutside(item: OutsideItem) {
		const next = revealMonitor({ env, services }, item);
		if (next.env !== env) setEnv(next.env);
		if (next.services !== services) setServices(next.services);
	}

	async function fullSync() {
		try {
			const res = await fetchMonitors(90);
			// A merge, not a blind replace: a slow poll response can resolve
			// after a newer SSE event already updated the same monitor, and
			// a plain replace would silently regress it back to stale data.
			monitors = mergeMonitors(monitors, res.monitors);
			intervalSeconds = res.intervalSeconds;
			hasLoadedOnce = true;
			lastSuccessAt = Date.now();
		} catch (err) {
			if (err instanceof UnauthorizedError) return;
			console.error('failed to load monitors', err);
			lastFailureAt = Date.now();
		}
	}

	// The full 90-day resync. Every caller keeps calling this (not fullSync
	// directly): it's the same signature/behavior, just deduplicated so
	// several resync triggers around the same moment (e.g. an SSE reconnect
	// right after the recent-poll already spotted a gap) share one request.
	function loadMonitors(): Promise<void> {
		fullSyncInFlight ??= fullSync().finally(() => {
			fullSyncInFlight = null;
		});
		return fullSyncInFlight;
	}

	// The 60s poll's own path: fetches only each monitor's last couple of
	// days instead of the full 90, falling back to a full resync whenever
	// the delta can't be merged safely. Skips entirely while a full sync is
	// already in flight, so it never races a resync some other caller
	// already triggered.
	async function pollRecent() {
		if (fullSyncInFlight) return;
		try {
			const res = await fetchMonitors(90, 'recent');
			if (res.history === 'recent') {
				const result = mergeRecentMonitors(monitors, res.monitors);
				monitors = result.monitors;
				if (result.needsResync) {
					await loadMonitors();
					return;
				}
			} else {
				// Defensive: the server is expected to always honor
				// history=recent, but if it ever answers full anyway, merge
				// it the same way loadMonitors does instead of feeding a
				// 90-day response through the delta path.
				monitors = mergeMonitors(monitors, res.monitors);
			}
			intervalSeconds = res.intervalSeconds;
			hasLoadedOnce = true;
			lastSuccessAt = Date.now();
		} catch (err) {
			if (err instanceof UnauthorizedError) return;
			console.error('failed to poll monitors', err);
			lastFailureAt = Date.now();
		}
	}

	function startPolling() {
		stopPolling();
		pollHandle = setInterval(pollRecent, POLL_MS);
	}

	function stopPolling() {
		if (pollHandle) clearInterval(pollHandle);
		pollHandle = undefined;
	}

	// The real-time path: pushes a monitor's fresh state the moment a check
	// completes, instead of every open tab re-polling everything on a timer.
	// The (much slower) poll above stays on as a reconciliation backstop.
	function startEvents() {
		stopEvents();
		streamDropped = false;
		eventsClose = subscribeToMonitorEvents(
			(m) => {
				const result = applyMonitorDelta(monitors, m);
				monitors = result.monitors;
				if (result.needsResync) loadMonitors();
				lastSuccessAt = Date.now();
			},
			(ok) => {
				if (ok) {
					// Only the first reconnect signal after a drop repairs
					// whatever the client missed while disconnected -- not
					// every 25s heartbeat that follows.
					if (streamDropped) {
						streamDropped = false;
						loadMonitors();
					}
					lastSuccessAt = Date.now();
				} else {
					streamDropped = true;
					lastFailureAt = Date.now();
				}
			}
		);
	}

	function stopEvents() {
		eventsClose?.();
		eventsClose = null;
	}

	// Closing the events stream while the tab is hidden, and doing a full
	// reload (not just reopening the stream) when it becomes visible again,
	// matters for a real reason: browsers cap same-origin connections at
	// around 6 over HTTP/1.1, and an SSE connection stays open indefinitely
	// -- past a handful of background tabs, everything else (the poll, a
	// manual check) would start hanging.
	//
	// But "hidden" fires on every quick alt-tab or window switch (on macOS,
	// even the tab's own window getting occluded by another one), not just
	// on a tab staying backgrounded for a while -- tearing the stream down
	// immediately on every one of those turns this into a full reload plus
	// a fresh connection each time, which for someone bouncing between
	// windows moves *more* traffic than the poll this feature replaced.
	// Debouncing the teardown means a quick round trip costs nothing: the
	// stream was never actually closed, so there's nothing to reload.
	const HIDDEN_TEARDOWN_MS = 90000;

	function handleVisibilityChange() {
		tabVisible = document.visibilityState === 'visible';
		if (document.visibilityState === 'hidden') {
			if (hiddenTeardownHandle) clearTimeout(hiddenTeardownHandle);
			hiddenTeardownHandle = setTimeout(stopEvents, HIDDEN_TEARDOWN_MS);
			return;
		}

		if (hiddenTeardownHandle) {
			clearTimeout(hiddenTeardownHandle);
			hiddenTeardownHandle = undefined;
		}
		if (authenticated && eventsClose === null) {
			loadMonitors();
			startEvents();
		}
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
			startEvents();
		} catch (err) {
			gateError = err instanceof UnauthorizedError ? 'Token inválido' : 'No se pudo conectar con el servidor';
		} finally {
			gateSubmitting = false;
		}
	}

	function handleUnauthorized() {
		stopPolling();
		stopEvents();
		authenticated = false;
		monitors = [];
		hasLoadedOnce = false;
		lastSuccessAt = null;
		lastFailureAt = null;
	}

	function closeForm() {
		formOpen = false;
		editingMonitor = null;
		formError = null;
	}

	function openCreateForm(prefill: FormPrefill) {
		editingMonitor = null;
		formError = null;
		formPrefill = prefill;
		formOpen = true;
	}

	function toggleCreateForm() {
		if (formOpen || editingMonitor) {
			closeForm();
		} else {
			openCreateForm({ service: null, kind: null, environment: env === 'all' ? null : env, note: null });
		}
	}

	function handleAddToService(service: string) {
		openCreateForm({ service, kind: null, environment: env === 'all' ? null : env, note: null });
	}

	function handleAddFromBoard(service: string, kind: MonitorKind, environment: Environment) {
		openCreateForm({ service, kind, environment, note: BOARD_PREFILL_NOTE });
	}

	function handleEdit(monitor: Monitor) {
		formOpen = false;
		formError = null;
		editingMonitor = monitor;
	}

	async function handleFormSubmit(input: NewMonitorInput) {
		formSubmitting = true;
		formError = null;
		try {
			if (editingMonitor) {
				const updated = await updateMonitor(editingMonitor.id, {
					name: input.name,
					description: input.description,
					service: input.service,
					kind: input.kind,
					environment: input.environment
				});
				monitors = upsertMonitor(monitors, updated);
			} else {
				const created = await createMonitor(input);
				// Never blindly append: a concurrent poll or SSE event that
				// resolves around the same time can already include this
				// monitor, and appending it again would leave two entries
				// with the same id, crashing the keyed each-block.
				monitors = upsertMonitor(monitors, created);
			}
			closeForm();
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
			monitors = upsertMonitor(monitors, updated);
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
			if (isView(savedView)) view = savedView;
			const savedEnv = localStorage.getItem(ENV_KEY);
			if (isEnvFilter(savedEnv)) env = savedEnv;
			services = parseServices(localStorage.getItem(SERVICES_KEY));
			days = parsePeriod(localStorage.getItem(DAYS_KEY));
		} catch {
			// Storage unavailable: defaults to the list view, every
			// environment and service, and 90 days for this tab.
		}
		const unsubscribe = onUnauthorized(handleUnauthorized);
		tabVisible = document.visibilityState === 'visible';
		document.addEventListener('visibilitychange', handleVisibilityChange);
		nowTickHandle = setInterval(() => {
			now = Date.now();
		}, NOW_TICK_MS);

		if (authenticated) {
			loadMonitors();
			startPolling();
			startEvents();
		}

		return () => {
			unsubscribe();
			document.removeEventListener('visibilitychange', handleVisibilityChange);
			if (nowTickHandle) clearInterval(nowTickHandle);
			if (hiddenTeardownHandle) clearTimeout(hiddenTeardownHandle);
			stopPolling();
			stopEvents();
		};
	});
</script>

<svelte:head>
	<title>Estado de servicios · GOIAR</title>
</svelte:head>

{#if !authenticated}
	<TokenGate error={gateError} submitting={gateSubmitting} onSubmit={handleGateSubmit} />
{:else}
	<div class="page">
		<header class="page-header">
			<div class="titles">
				<span class="eyebrow">GOIAR</span>
				<h1>Estado de servicios</h1>
			</div>
			<div class="actions">
				<div class="view-switch" role="group" aria-label="Vista">
					{#each VIEW_OPTIONS as option (option.id)}
						<button
							type="button"
							class="view-option"
							class:active={view === option.id}
							aria-pressed={view === option.id}
							onclick={() => setView(option.id)}
						>
							<Icon name={option.icon} />
							{option.label}
						</button>
					{/each}
				</div>
				<ThemeToggle {theme} onToggle={toggleTheme} />
				<button type="button" onclick={handleCheckAll}>
					<Icon name="refresh-line" />
					Verificar todas
				</button>
				<button type="button" class="primary" onclick={toggleCreateForm}>
					<Icon name="add-line" />
					Agregar monitor
				</button>
			</div>
		</header>

		<div class="layout" class:with-rail={wideLayout.current}>
			{#if wideLayout.current}
				<aside class="rail" aria-label="Filtros">
					<FilterRail
						{env}
						envCounts={envMonitorCounts}
						items={serviceItems}
						{services}
						onEnvChange={setEnv}
						onToggleService={handleToggleService}
						onClearServices={clearServices}
					/>
				</aside>
			{:else}
				<FilterBar
					{env}
					envCounts={envMonitorCounts}
					items={serviceItems}
					{services}
					onEnvChange={setEnv}
					onToggleService={handleToggleService}
					onClearServices={clearServices}
				/>
			{/if}

			<div class="content">
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
					<StatusBanner
						monitors={visibleMonitors}
						all={monitors}
						{env}
						{services}
						{intervalSeconds}
						onFocusService={focusService}
						onReveal={revealOutside}
						onClearFilters={clearFilters}
					/>
				{/if}

				{#if formOpen || editingMonitor}
					{#key formKey}
						<AddMonitorForm
							{editingMonitor}
							services={knownServices}
							defaultService={formPrefill.service}
							defaultKind={formPrefill.kind}
							defaultEnvironment={formPrefill.environment}
							prefillNote={formPrefill.note}
							error={formError}
							submitting={formSubmitting}
							onCancel={closeForm}
							onSubmit={handleFormSubmit}
						/>
					{/key}
				{/if}

				{#if !hasLoadedOnce && connectionError}
					<section class="monitor-list-section">
						<div class="list-caption">Disponibilidad de los últimos {days} días</div>
						<div class="monitor-list">
							<div class="empty-state">
								<Icon name="pulse-line" size={32} />
								<p class="empty-title">No se pudo conectar con el servidor</p>
								<p class="empty-sub">Reintentando automáticamente…</p>
							</div>
						</div>
					</section>
				{:else if hasLoadedOnce && visibleMonitors.length === 0}
					<section class="monitor-list-section">
						<div class="monitor-list">
							<div class="empty-state">
								<Icon name="pulse-line" size={32} />
								<p class="empty-title">{emptyCopy.title}</p>
								<p class="empty-sub">{emptyCopy.sub}</p>
								{#if emptyCopy.canClear}
									<button type="button" class="empty-action" onclick={clearFilters}>Ver todos</button>
								{/if}
							</div>
						</div>
					</section>
				{:else if hasLoadedOnce && view === 'chart'}
					<ChartView monitors={visibleMonitors} {tabVisible} />
				{:else if hasLoadedOnce && view === 'board'}
					<MonitorBoard
						monitors={periodMonitors}
						{env}
						{days}
						onDaysChange={setDays}
						onCheck={handleCheck}
						onAdd={handleAddFromBoard}
					/>
				{:else if hasLoadedOnce}
					<MonitorList
						monitors={periodMonitors}
						showEnv={env === 'all'}
						{days}
						twoColumns={twoColumnList.current}
						scopeTitle={wideLayout.current ? scopeTitle(env, services) : null}
						onDaysChange={setDays}
						onAddToService={handleAddToService}
						onCheck={handleCheck}
						onEdit={handleEdit}
						onDelete={handleDelete}
					/>
				{/if}
			</div>
		</div>

		<footer class="build-info">
			Front v{PUBLIC_WEB_VERSION} · API v{PUBLIC_API_VERSION} · commit {PUBLIC_GIT_COMMIT}
		</footer>
	</div>
{/if}

<style>
	.page {
		max-width: 1760px;
		margin: 0 auto;
		padding: 48px 24px 80px;
		display: flex;
		flex-direction: column;
		gap: 32px;
	}

	@media (min-width: 1024px) {
		.page {
			padding: 48px 40px 80px;
		}
	}

	.layout,
	.content {
		display: flex;
		flex-direction: column;
		gap: 32px;
		min-width: 0;
	}

	.layout.with-rail {
		display: grid;
		grid-template-columns: 264px minmax(0, 1fr);
		align-items: start;
	}

	.rail {
		display: flex;
		flex-direction: column;
		gap: 28px;
		min-width: 0;
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
		flex-wrap: wrap;
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

	/* Scoped like .actions > button, or that rule's higher specificity would
	   override these colors. */
	.actions > .primary {
		border: 0;
		background: var(--primary-500);
		color: #fff;
	}

	.actions > .primary:hover {
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
		color: var(--m-sub-tx);
	}

	.view-option.active {
		background: var(--m-ink);
		color: var(--m-surface);
	}

	/* Keeps the three views within a 320px phone without page scroll. */
	@media (max-width: 400px) {
		.view-option {
			padding: 0 8px;
		}
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

	.empty-action {
		margin-top: 8px;
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

	.empty-action:hover {
		background: var(--m-soft);
	}

	.build-info {
		text-align: center;
		font-size: 12px;
		color: var(--fg-3);
		font-family: var(--font-mono);
	}
</style>
