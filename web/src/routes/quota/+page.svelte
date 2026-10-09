<script lang="ts">
	import { ApiError, errorMessage, publicQuota, quotaMeta, type QuotaView } from '#lib/api.js';
	import { relativeTime } from '#lib/format.js';
	import QuotaViewComponent from '#lib/components/quota-view.svelte';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { onMount } from 'svelte';

	const storageKey = 'maitake:quota-key';

	let meta = $state<{ enabled: boolean; access: 'key' | 'public'; title: string } | null>(null);
	let key = $state<string | null>(null);
	let keyInput = $state('');
	let view = $state<QuotaView | null>(null);
	let error = $state('');
	let loading = $state(true);

	async function load() {
		try {
			view = await publicQuota(key);
			error = '';
		} catch (e) {
			if (e instanceof ApiError && e.status === 401 && key) {
				localStorage.removeItem(storageKey);
				key = null;
				error = 'That key was not accepted.';
				view = null;
			} else if (!(e instanceof ApiError && e.status === 401)) {
				error = errorMessage(e);
			}
		} finally {
			loading = false;
		}
	}

	onMount(() => {
		key = localStorage.getItem(storageKey);
		quotaMeta().then((m) => {
			meta = m;
			if (m.enabled && (m.access === 'public' || key)) load();
			else loading = false;
		});
		const t = setInterval(() => {
			if (view) load();
		}, 60_000);
		return () => clearInterval(t);
	});

	function submitKey(e: SubmitEvent) {
		e.preventDefault();
		key = keyInput.trim();
		localStorage.setItem(storageKey, key);
		loading = true;
		load();
	}

	function forget() {
		localStorage.removeItem(storageKey);
		key = null;
		keyInput = '';
		if (meta?.access === 'public') load();
		else view = null;
	}
</script>

<svelte:head><title>{meta?.title || 'Quota'}</title></svelte:head>

<main class="mx-auto min-h-svh max-w-3xl space-y-6 px-4 py-10">
	<div class="flex flex-wrap items-end justify-between gap-3">
		<div>
			<h1 class="text-2xl font-semibold tracking-tight">{view?.title || meta?.title || 'Quota'}</h1>
			{#if view}<p class="text-sm text-muted-foreground">
					Updated {relativeTime(view.generatedAt)}
				</p>{/if}
		</div>
		{#if key}<Button variant="ghost" size="sm" onclick={forget}>Use a different key</Button>{/if}
	</div>

	{#if loading}
		<p class="text-sm text-muted-foreground">Loading…</p>
	{:else if meta && !meta.enabled}
		<p class="text-sm text-muted-foreground">This page isn't available.</p>
	{:else if !view && meta?.access === 'key'}
		<Card.Root class="max-w-md">
			<Card.Header>
				<Card.Title>Enter your API key</Card.Title>
				<Card.Description
					>It stays in this browser and is only used to load this page.</Card.Description
				>
			</Card.Header>
			<Card.Content>
				<form onsubmit={submitKey} class="space-y-4">
					<Field.Field data-invalid={!!error}>
						<Input
							type="password"
							placeholder="mtk_…"
							autocomplete="off"
							required
							bind:value={keyInput}
							class="font-mono"
						/>
						{#if error}<Field.Error>{error}</Field.Error>{/if}
					</Field.Field>
					<Button type="submit">Show quota</Button>
				</form>
			</Card.Content>
		</Card.Root>
	{:else if error}
		<p class="text-sm text-destructive">{error}</p>
	{:else if view}
		<QuotaViewComponent {view} />
	{/if}
</main>
