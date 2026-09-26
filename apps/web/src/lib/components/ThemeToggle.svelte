<script lang="ts">
	import Icon from './Icon.svelte';

	let { theme, onToggle }: { theme: 'light' | 'dark'; onToggle: () => void } = $props();
</script>

<button type="button" class="icon-button" title="Usar modo oscuro/claro" onclick={onToggle}>
	<Icon name={theme === 'dark' ? 'sun-line' : 'moon-line'} />
</button>

<style>
	.icon-button {
		display: inline-flex;
		align-items: center;
		justify-content: center;
		width: 40px;
		height: 40px;
		padding: 0;
		border: 1px solid var(--border-default);
		border-radius: 4px;
		background: var(--m-surface);
		color: var(--m-ink);
		font-size: 18px;
		cursor: pointer;
	}

	.icon-button:hover {
		background: var(--m-soft);
	}
</style>
