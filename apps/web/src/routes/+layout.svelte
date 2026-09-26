<script lang="ts">
	import '@fontsource/sora/400.css';
	import '@fontsource/sora/600.css';
	import '@fontsource/sora/700.css';
	import '../app.css';
	import favicon from '$lib/assets/favicon.svg';

	let { children } = $props();
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
</svelte:head>

{@render children()}
