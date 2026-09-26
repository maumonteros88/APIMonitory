<script lang="ts">
	let {
		error,
		submitting,
		onSubmit
	}: {
		error: string | null;
		submitting: boolean;
		onSubmit: (token: string) => void;
	} = $props();

	let token = $state('');

	function handleSubmit(event: SubmitEvent) {
		event.preventDefault();
		const trimmed = token.trim();
		if (!trimmed) return;
		onSubmit(trimmed);
	}
</script>

<div class="gate">
	<form class="gate-card" onsubmit={handleSubmit}>
		<span class="gate-eyebrow">GOIAR</span>
		<h1>Estado de APIs</h1>

		<label>
			Token de acceso
			<input type="password" bind:value={token} autocomplete="off" required />
		</label>

		{#if error}
			<p class="gate-error">{error}</p>
		{/if}

		<button type="submit" class="primary" disabled={submitting}>Continuar</button>
	</form>
</div>

<style>
	.gate {
		min-height: 100vh;
		display: flex;
		align-items: center;
		justify-content: center;
		padding: 24px;
	}

	.gate-card {
		width: 100%;
		max-width: 360px;
		display: flex;
		flex-direction: column;
		gap: 16px;
		padding: 32px;
		background: var(--m-surface);
		border: 1px solid var(--border-default);
		border-radius: 8px;
	}

	.gate-eyebrow {
		font-size: 12px;
		font-weight: 600;
		color: var(--fg-2);
		letter-spacing: 0.04em;
	}

	.gate-card h1 {
		margin: 0;
		font-size: 24px;
		font-weight: 700;
	}

	label {
		display: flex;
		flex-direction: column;
		gap: 6px;
		font-size: 14px;
		font-weight: 600;
	}

	input {
		font: inherit;
		font-weight: 400;
		padding: 10px 12px;
		border: 1px solid var(--border-default);
		border-radius: 6px;
		background: var(--bg-app);
		color: var(--fg-1);
	}

	.gate-error {
		margin: 0;
		font-size: 14px;
		color: var(--m-bad-tx);
	}

	.primary {
		background: var(--primary-500);
		color: #fff;
	}
</style>
