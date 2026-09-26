<script lang="ts">
	import type { NewMonitorInput } from '$lib/api';
	import Icon from './Icon.svelte';

	let {
		error,
		submitting,
		onCancel,
		onSubmit
	}: {
		error: string | null;
		submitting: boolean;
		onCancel: () => void;
		onSubmit: (input: NewMonitorInput) => void;
	} = $props();

	let name = $state('');
	let url = $state('');
	let description = $state('');

	function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		onSubmit({ name: name.trim(), url: url.trim(), description: description.trim() });
	}
</script>

<form class="add-form" onsubmit={handleSubmit}>
	<div class="form-header">
		<h2>Nueva API</h2>
		<button type="button" class="icon-button" onclick={onCancel} aria-label="Cerrar formulario">
			<Icon name="close-line" />
		</button>
	</div>

	<label>
		Nombre
		<input type="text" placeholder="API de pagos" bind:value={name} required />
	</label>

	<label>
		URL de health
		<input type="text" placeholder="https://api.ejemplo.com/health" bind:value={url} required />
	</label>

	<label>
		Descripción
		<textarea rows="2" placeholder="Qué hace esta API y quién la usa" bind:value={description}
		></textarea>
	</label>

	{#if error}
		<p class="form-error">
			<Icon name="error-warning-line" size={16} />
			{error}
		</p>
	{/if}

	<div class="form-actions">
		<button type="button" class="ghost" onclick={onCancel}>Cancelar</button>
		<button type="submit" class="primary" disabled={submitting}>Guardar API</button>
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

	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 14px;
		font-weight: 600;
	}

	input,
	textarea {
		font: inherit;
		font-size: 16px;
		font-weight: 400;
		height: 44px;
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
