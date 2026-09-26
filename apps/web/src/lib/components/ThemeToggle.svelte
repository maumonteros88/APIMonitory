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
	}
</style>
