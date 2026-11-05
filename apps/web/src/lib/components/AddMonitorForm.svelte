<script lang="ts">
	import { untrack } from 'svelte';
	import type { Environment, Monitor, MonitorKind, NewMonitorInput } from '$lib/api';
	import Icon from './Icon.svelte';

	// When set, the form edits this monitor instead of creating a new one.
	// Its URL is shown but locked: changing the probe target is a bigger
	// change than a rename and isn't part of this form. services feeds the
	// Servicio suggestions; defaultEnvironment preselects Ambiente on create.
	let {
		editingMonitor = null,
		services,
		defaultEnvironment,
		error,
		submitting,
		onCancel,
		onSubmit
	}: {
		editingMonitor?: Monitor | null;
		services: string[];
		defaultEnvironment: Environment | null;
		error: string | null;
		submitting: boolean;
		onCancel: () => void;
		onSubmit: (input: NewMonitorInput) => void;
	} = $props();

	const KIND_OPTIONS: { value: MonitorKind; label: string }[] = [
		{ value: 'front', label: 'Front' },
		{ value: 'api', label: 'API' }
	];

	const ENV_OPTIONS: { value: Environment | null; label: string }[] = [
		{ value: null, label: 'Sin asignar' },
		{ value: 'dev', label: 'Dev' },
		{ value: 'qa', label: 'QA' },
		{ value: 'prod', label: 'Prod' }
	];

	const uid = $props.id();

	// The page re-creates this form (keyed on the monitor id) for each
	// monitor it edits, so the fields only take the props' initial values.
	const initial = untrack(() => ({ monitor: editingMonitor, environment: defaultEnvironment }));

	let name = $state(initial.monitor?.name ?? '');
	let url = $state(initial.monitor?.url ?? '');
	let description = $state(initial.monitor?.description ?? '');
	let service = $state(initial.monitor?.service ?? '');
	let kind = $state<MonitorKind>(initial.monitor?.kind ?? 'api');
	let environment = $state<Environment | null>(
		initial.monitor ? initial.monitor.environment : initial.environment
	);

	const urlHelp = $derived(
		kind === 'front'
			? 'Para un front alcanza con la URL del sitio: se considera operativo si responde 2xx.'
			: 'El endpoint de health de la API. Si devuelve un JSON con "commit", se muestra en la tarjeta.'
	);

	function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		onSubmit({
			name: name.trim(),
			url: url.trim(),
			description: description.trim(),
			service: service.trim() || null,
			kind,
			environment
		});
	}
</script>

<form class="add-form" onsubmit={handleSubmit}>
	<div class="form-header">
		<h2>{editingMonitor ? 'Editar monitor' : 'Nuevo monitor'}</h2>
		<button type="button" class="icon-button" onclick={onCancel} aria-label="Cerrar formulario">
			<Icon name="close-line" />
		</button>
	</div>

	<div class="form-grid">
		<div class="field">
			<label for="{uid}-name">Nombre</label>
			<input id="{uid}-name" type="text" placeholder="API de pagos" bind:value={name} required />
		</div>

		<div class="field">
			<label for="{uid}-url">URL a verificar</label>
			<input
				id="{uid}-url"
				type="text"
				placeholder="https://api.ejemplo.com/health"
				aria-describedby="{uid}-url-help"
				bind:value={url}
				required
				disabled={!!editingMonitor}
			/>
			<span id="{uid}-url-help" class="help">{urlHelp}</span>
		</div>

		<div class="field">
			<span id="{uid}-kind" class="field-label">Tipo</span>
			<div class="segmented" role="group" aria-labelledby="{uid}-kind">
				{#each KIND_OPTIONS as option (option.label)}
					<button
						type="button"
						class="segmented-option"
						class:active={kind === option.value}
						aria-pressed={kind === option.value}
						onclick={() => (kind = option.value)}
					>
						{option.label}
					</button>
				{/each}
			</div>
		</div>

		<div class="field">
			<span id="{uid}-env" class="field-label">Ambiente</span>
			<div class="segmented" role="group" aria-labelledby="{uid}-env">
				{#each ENV_OPTIONS as option (option.label)}
					<button
						type="button"
						class="segmented-option"
						class:active={environment === option.value}
						aria-pressed={environment === option.value}
						onclick={() => (environment = option.value)}
					>
						{option.label}
					</button>
				{/each}
			</div>
		</div>
	</div>

	<div class="field">
		<label for="{uid}-service">Servicio <span class="optional">(opcional)</span></label>
		<input
			id="{uid}-service"
			type="text"
			placeholder="Pagos"
			list="{uid}-services"
			maxlength="80"
			autocomplete="off"
			aria-describedby="{uid}-service-help"
			bind:value={service}
		/>
		<datalist id="{uid}-services">
			{#each services as option (option)}
				<option value={option}></option>
			{/each}
		</datalist>
		<span id="{uid}-service-help" class="help">
			Agrupa el front y la API de un mismo servicio. Elige uno existente o escribe uno nuevo.
		</span>
	</div>

	<div class="field">
		<label for="{uid}-description">Descripción <span class="optional">(opcional)</span></label>
		<textarea
			id="{uid}-description"
			rows="2"
			placeholder="Qué hace y quién lo usa"
			bind:value={description}
		></textarea>
	</div>

	{#if error}
		<p class="form-error">
			<Icon name="error-warning-line" size={16} />
			{error}
		</p>
	{/if}

	<div class="form-actions">
		<button type="button" class="ghost" onclick={onCancel}>Cancelar</button>
		<button type="submit" class="primary" disabled={submitting}>
			{editingMonitor ? 'Guardar cambios' : 'Guardar monitor'}
		</button>
	</div>
</form>

<style>
	.add-form {
		padding: 24px;
		background: var(--m-surface);
		border: 1px solid var(--border-default);
		border-radius: 8px;
		display: flex;
		flex-direction: column;
		gap: 16px;
	}

	.form-header {
		display: flex;
		align-items: center;
		justify-content: space-between;
	}

	.form-header h2 {
		margin: 0;
	}

	.icon-button {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		border: 0;
		background: none;
		padding: 4px;
		color: var(--fg-2);
		font-size: 20px;
		cursor: pointer;
	}

	/* Two columns at most (Nombre | URL, Tipo | Ambiente), one on narrow
	   screens. */
	.form-grid {
		display: grid;
		grid-template-columns: repeat(auto-fit, minmax(max(260px, calc(50% - 8px)), 1fr));
		gap: 16px;
		align-items: start;
	}

	.field {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}

	label,
	.field-label {
		font-size: 14px;
		font-weight: 600;
	}

	.optional {
		font-weight: 400;
		color: var(--fg-2);
	}

	.help {
		font-size: 12px;
		color: var(--fg-2);
	}

	input,
	textarea {
		font: inherit;
		font-size: 16px;
		font-weight: 400;
		height: 44px;
		box-sizing: border-box;
		padding: 0 12px;
		border: 1px solid var(--border-default);
		border-radius: 4px;
		background: var(--bg-app);
		color: var(--fg-1);
		outline: none;
	}

	textarea {
		height: auto;
		padding: 10px 12px;
		resize: vertical;
	}

	input:focus,
	textarea:focus {
		border: 2px solid var(--primary-500);
	}

	input:focus {
		padding: 0 11px;
	}

	textarea:focus {
		padding: 9px 11px;
	}

	input:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}

	.segmented {
		display: flex;
		height: 44px;
		box-sizing: border-box;
		padding: 3px;
		gap: 2px;
		border: 1px solid var(--border-default);
		border-radius: 4px;
		background: var(--bg-app);
	}

	.segmented-option {
		flex: 1;
		border: 0;
		border-radius: 2px;
		background: transparent;
		color: var(--fg-2);
		font-size: 14px;
		font-weight: 700;
		white-space: nowrap;
		cursor: pointer;
	}

	.segmented-option.active {
		background: var(--m-ink);
		color: var(--m-surface);
	}

	.form-error {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 14px;
		color: var(--m-bad-tx);
		margin: 0;
	}

	.form-actions {
		display: flex;
		justify-content: flex-end;
		gap: 8px;
	}

	.form-actions button {
		height: 40px;
		padding: 0 16px;
		border: 0;
		border-radius: 4px;
		font-size: 14px;
		font-weight: 700;
		cursor: pointer;
	}

	.ghost {
		background: none;
		color: var(--m-ink);
	}

	.primary {
		padding: 0 20px;
		background: var(--primary-500);
		color: #fff;
	}

	.primary:hover {
		background: var(--primary-700);
	}
</style>
