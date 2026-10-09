<script lang="ts">
	import './layout.css';
	import favicon from '#lib/assets/favicon.png';
	import { ModeWatcher } from 'mode-watcher';
	import { Toaster } from '#lib/components/ui/sonner/index.js';
	import * as Tooltip from '#lib/components/ui/tooltip/index.js';
	import type { LayoutProps } from './$types';

	let { children }: LayoutProps = $props();
</script>

<svelte:head>
	<title>maitake</title>
	<link rel="icon" type="image/png" href={favicon} />
	<link rel="apple-touch-icon" href="/apple-touch-icon.png" />
</svelte:head>

<ModeWatcher />
<Toaster richColors />
<Tooltip.Provider>
	{@render children()}
</Tooltip.Provider>
