<script lang="ts">
	import type { AuditBody } from '#lib/types.js';
	import { bytes } from '#lib/format.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import DownloadIcon from '@lucide/svelte/icons/download';
	import { toast } from 'svelte-sonner';

	let { body, filename }: { body: AuditBody; filename: string } = $props();

	let mode = $state<'pretty' | 'raw'>('pretty');

	function prettyJSON(text: string): string | undefined {
		try {
			return JSON.stringify(JSON.parse(text), null, 2);
		} catch {
			return undefined;
		}
	}

	// Server-sent events are shown one per block, each event's data pretty-printed.
	function prettySSE(text: string): string | undefined {
		if (!/^(event|data):/m.test(text)) return undefined;
		return text
			.split(/\r?\n\r?\n/)
			.filter((block) => block.trim())
			.map((block) => {
				const lines = block.split(/\r?\n/);
				const data = lines
					.filter((l) => l.startsWith('data:'))
					.map((l) => l.slice(5).trimStart())
					.join('\n');
				const head = lines.filter((l) => !l.startsWith('data:'));
				const parsed = data ? (prettyJSON(data) ?? data) : '';
				return [...head, parsed].filter(Boolean).join('\n');
			})
			.join('\n\n');
	}

	const pretty = $derived(prettyJSON(body.text) ?? prettySSE(body.text));
	const shown = $derived(mode === 'pretty' && pretty !== undefined ? pretty : body.text);

	async function copy() {
		try {
			await navigator.clipboard.writeText(body.text);
			toast.success('Copied');
		} catch {
			toast.error("Couldn't copy");
		}
	}

	function download() {
		const url = URL.createObjectURL(new Blob([body.text], { type: 'text/plain' }));
		const a = document.createElement('a');
		a.href = url;
		a.download = filename;
		a.click();
		URL.revokeObjectURL(url);
	}
</script>

<div class="space-y-2">
	<div class="flex flex-wrap items-center gap-2">
		<span class="text-sm text-muted-foreground">{bytes(body.size)}</span>
		{#if body.truncated}
			<Badge variant="secondary" title="Only the first part was kept"
				>cut off at {bytes(new TextEncoder().encode(body.text).length)}</Badge
			>
		{/if}
		<div class="ml-auto flex items-center gap-2">
			{#if pretty !== undefined}
				<ToggleGroup.Root
					type="single"
					variant="outline"
					size="sm"
					value={mode}
					onValueChange={(v) => v && (mode = v as 'pretty' | 'raw')}
				>
					<ToggleGroup.Item value="pretty" class="px-3">Formatted</ToggleGroup.Item>
					<ToggleGroup.Item value="raw" class="px-3">Raw</ToggleGroup.Item>
				</ToggleGroup.Root>
			{/if}
			<Button variant="outline" size="sm" onclick={copy}><CopyIcon /> Copy</Button>
			<Button variant="outline" size="sm" onclick={download}><DownloadIcon /> Download</Button>
		</div>
	</div>
	{#if body.text}
		<pre
			class="max-h-[70vh] overflow-auto rounded-md border bg-muted/40 p-3 font-mono text-xs leading-relaxed break-all whitespace-pre-wrap">{shown}</pre>
	{:else}
		<p class="py-4 text-sm text-muted-foreground">Empty.</p>
	{/if}
</div>
