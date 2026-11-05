<script lang="ts">
	import { onMount, untrack } from 'svelte';
	import { ASSIGN_COMPANY_ATTR } from '$lib/companies';
	import Icon from './Icon.svelte';

	// Sets the company of every monitor of one service. A native modal
	// <dialog>: the browser keeps focus inside it and closes it on Escape
	// (which only asks the page to close it, see handleCancel). current is
	// the service's company now, companies feeds the suggestions and
	// monitorCount is how many monitors the change reaches. onSubmit gets the
	// trimmed company, or null to remove it.
	let {
		service,
		current,
		monitorCount,
		companies,
		error,
		submitting,
		onCancel,
		onSubmit
	}: {
		service: string;
		current: string | null;
		monitorCount: number;
		companies: string[];
		error: string | null;
		submitting: boolean;
		onCancel: () => void;
		onSubmit: (company: string | null) => void;
	} = $props();

	const uid = $props.id();

	// The page creates this dialog for one service and discards it after, so
	// the field only reads the initial company.
	let company = $state(untrack(() => current ?? ''));
	let dialog = $state<HTMLDialogElement>();
	let input = $state<HTMLInputElement>();

	const help = $derived(
		monitorCount === 1
			? `Se aplica al monitor de ${service}`
			: `Se aplica a los ${monitorCount} monitores de ${service}`
	);

	onMount(() => {
		const opener = document.activeElement instanceof HTMLElement ? document.activeElement : null;
		// Read now: by teardown the page has already cleared the service prop.
		const assigned = service;
		dialog?.showModal();
		input?.select();
		return () => {
			if (dialog?.open) dialog.close();
			// Once saved, the service can sit in another company section, so its
			// button may be a new element: look it up once the page has
			// re-rendered and prefer it over the opener, which Safari leaves as
			// <body> because a click there doesn't focus buttons.
			requestAnimationFrame(() => {
				const button = document.querySelector<HTMLElement>(`[${ASSIGN_COMPANY_ATTR}="${CSS.escape(assigned)}"]`);
				(button ?? (opener?.isConnected && opener !== document.body ? opener : null))?.focus();
			});
		};
	});

	function handleCancel(event: Event) {
		event.preventDefault();
		if (!submitting) onCancel();
	}

	function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		onSubmit(company.trim() || null);
	}
</script>

<dialog bind:this={dialog} class="company-dialog" aria-labelledby="{uid}-title" oncancel={handleCancel}>
	<form onsubmit={handleSubmit}>
		<div class="dialog-header">
			<h2 id="{uid}-title">Empresa de {service}</h2>
		</div>

		<div class="field">
			<label for="{uid}-company">Empresa</label>
			<input
				id="{uid}-company"
				type="text"
				placeholder="Empresa A"
				list="{uid}-companies"
				maxlength="80"
				autocomplete="off"
				aria-describedby="{uid}-help"
				bind:value={company}
				bind:this={input}
			/>
			<datalist id="{uid}-companies">
				{#each companies as option (option)}
					<option value={option}></option>
				{/each}
			</datalist>
			<span id="{uid}-help" class="help">{help}</span>
		</div>

		{#if error}
			<p class="form-error" role="alert">
				<Icon name="error-warning-line" size={16} />
				{error}
			</p>
		{/if}

		<div class="dialog-actions">
			{#if current !== null}
				<button type="button" class="ghost remove" disabled={submitting} onclick={() => onSubmit(null)}>
					Quitar empresa
				</button>
			{/if}
			<span class="spacer"></span>
			<button type="button" class="ghost" disabled={submitting} onclick={onCancel}>Cancelar</button>
			<button type="submit" class="primary" disabled={submitting}>Guardar</button>
		</div>
	</form>
</dialog>

<style>
	.company-dialog {
		width: 440px;
		max-width: calc(100vw - 32px);
		box-sizing: border-box;
		padding: 0;
		border: 1px solid var(--border-default);
		border-radius: 8px;
		background: var(--m-surface);
		color: var(--fg-1);
		box-shadow: 0 12px 28px -6px var(--m-shadow);
	}

	.company-dialog::backdrop {
		background: var(--m-shadow);
	}

	form {
		padding: 24px;
		display: flex;
		flex-direction: column;
		gap: 16px;
	}

	.dialog-header h2 {
		margin: 0;
		overflow-wrap: anywhere;
	}

	.field {
		display: flex;
		flex-direction: column;
		gap: 6px;
	}

	label {
		font-size: 14px;
		font-weight: 600;
	}

	.help {
		font-size: 12px;
		color: var(--fg-2);
		overflow-wrap: anywhere;
	}

	input {
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

	input:focus {
		border: 2px solid var(--primary-500);
		padding: 0 11px;
	}

	.form-error {
		display: flex;
		align-items: center;
		gap: 8px;
		font-size: 14px;
		color: var(--m-bad-tx);
		margin: 0;
	}

	.dialog-actions {
		display: flex;
		align-items: center;
		gap: 8px;
		flex-wrap: wrap;
	}

	.spacer {
		flex: 1;
	}

	.dialog-actions button {
		height: 40px;
		padding: 0 16px;
		border: 0;
		border-radius: 4px;
		font-size: 14px;
		font-weight: 700;
		cursor: pointer;
	}

	.dialog-actions button:disabled {
		opacity: 0.6;
		cursor: not-allowed;
	}

	.ghost {
		background: none;
		color: var(--m-ink);
	}

	.remove {
		padding: 0 8px;
		color: var(--m-sub-tx);
		text-decoration: underline;
		text-underline-offset: 3px;
	}

	.primary {
		padding: 0 20px;
		background: var(--primary-500);
		color: #fff;
	}

	.primary:hover:not(:disabled) {
		background: var(--primary-700);
	}
</style>
