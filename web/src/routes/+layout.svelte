<script lang="ts">
	import './layout.css';
	import favicon from '#lib/assets/favicon.svg';
	import { ModeWatcher } from 'mode-watcher';
	import { Toaster } from '#lib/components/ui/sonner/index.js';
	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();
</script>

<svelte:head>
	<title>maitake</title>
	<link rel="icon" href={favicon} />
</svelte:head>

<ModeWatcher />
<Toaster richColors />
{@render children()}
