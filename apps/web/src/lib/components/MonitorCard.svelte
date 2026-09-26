<script lang="ts">
	import type { Monitor } from '$lib/api';
	import { ago, statusMeta, uptimeLabel } from '$lib/format';
	import Icon from './Icon.svelte';
	import UptimeBars from './UptimeBars.svelte';

	let {
		monitor,
		onCheck,
		onDelete
	}: {
		monitor: Monitor;
		onCheck: (id: string) => void;
		onDelete: (id: string) => void;
	} = $props();

	const meta = $derived(statusMeta(monitor.status));

	const lastInfo = $derived.by(() => {
		if (monitor.checking) return 'Verificando…';
		if (!monitor.lastCheck) return 'Sin verificar';
		const { httpStatus, latencyMs, at } = monitor.lastCheck;
		return `HTTP ${httpStatus} · ${latencyMs} ms · ${ago(at)}`;
	});

	function handleDelete() {
		if (confirm(`¿Eliminar ${monitor.name}?`)) {
			onDelete(monitor.id);
		}
	}
</script>

<article class="monitor-card">
	<div class="monitor-header">
		<div class="monitor-info">
			<h3>{monitor.name}</h3>
			{#if monitor.description}<p class="monitor-desc">{monitor.description}</p>{/if}
			<span class="monitor-url">{monitor.url}</span>
		</div>
		<div class="monitor-status">
			<span class="status-line">
				<span class="status-dot" style="background: {meta.dotColor}"></span>
				<span style="color: {meta.textColor}">{meta.label}</span>
			</span>
			<span class="monitor-last-info">{lastInfo}</span>
		</div>
	</div>

	<UptimeBars history={monitor.history} />

	<div class="monitor-timeline-labels">
		<span>Hace {monitor.history.length} días</span>
		<span class="rule"></span>
		<span>{uptimeLabel(monitor.uptimePercent)}</span>
		<span class="rule"></span>
		<span>Hoy</span>
	</div>

	<div class="monitor-actions">
		<button type="button" class="check-action" onclick={() => onCheck(monitor.id)}>
			<Icon name="refresh-line" size={16} />
			Verificar ahora
		</button>
		<button type="button" class="delete-action" onclick={handleDelete}>
			<Icon name="delete-bin-line" size={16} />
			Eliminar
		</button>
	</div>
</article>

<style>
	.monitor-card {
		padding: 24px;
		display: flex;
		flex-direction: column;
		gap: 12px;
	}

	.monitor-header {
		display: flex;
		justify-content: space-between;
		gap: 16px;
		flex-wrap: wrap;
	}

	.monitor-info h3 {
		margin: 0;
	}

	.monitor-desc {
		margin: 4px 0 0;
		font-size: 14px;
		color: var(--fg-2);
	}

	.monitor-url {
		display: block;
		margin-top: 4px;
		font-size: 12px;
		color: var(--fg-3);
		font-family: var(--font-mono);
	}

	.monitor-status {
		text-align: right;
		display: flex;
		flex-direction: column;
		gap: 4px;
	}

	.status-line {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		font-size: 14px;
		font-weight: 600;
	}

	.status-dot {
		width: 10px;
		height: 10px;
		border-radius: 50%;
		flex-shrink: 0;
	}

	.monitor-last-info {
		font-size: 12px;
		color: var(--fg-2);
	}

	.monitor-timeline-labels {
		display: flex;
		align-items: center;
		gap: 16px;
		font-size: 14px;
		color: var(--fg-2);
	}

	.rule {
		flex: 1;
		height: 1px;
		background: var(--border-default);
	}

	.monitor-actions {
		display: flex;
		gap: 16px;
		font-size: 12px;
	}

	.monitor-actions button {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		border: 0;
		background: none;
		padding: 0;
		font-weight: 600;
		font-size: inherit;
		cursor: pointer;
	}

	.check-action {
		color: var(--m-ink);
	}

	.check-action:hover {
		color: var(--primary-700);
	}

	.delete-action {
		color: var(--fg-2);
	}

	.delete-action:hover {
		color: var(--danger-500);
	}
</style>
