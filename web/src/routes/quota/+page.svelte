<script lang="ts">
	import {
		ApiError,
		errorMessage,
		publicQuota,
		quotaMeta,
		type QuotaLayout,
		type QuotaView
	} from '#lib/api.js';
	import { relativeTime } from '#lib/format.js';
	import QuotaViewComponent from '#lib/components/quota-view.svelte';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import { pref } from '#lib/pref.svelte.js';
	import LayoutGridIcon from '@lucide/svelte/icons/layout-grid';
	import Rows3Icon from '@lucide/svelte/icons/rows-3';
	import { onMount } from 'svelte';

	const storageKey = 'maitake:quota-key';

	let meta = $state<{ enabled: boolean; access: 'key' | 'public'; title: string } | null>(null);
	let key = $state<string | null>(null);
	let keyInput = $state('');
	let view = $state<QuotaView | null>(null);
	let error = $state('');
	let loading = $state(true);

	const layoutPref = pref<QuotaLayout>('maitake:quota-page-view', ['cards', 'rows']);
	const layout = $derived(
		view ? (view.allowLayoutSwitch ? layoutPref.get(view.layout) : view.layout) : 'cards'
	);
	const width = $derived(
		view?.width === 'full' ? 'max-w-none' : view?.width === 'wide' ? 'max-w-6xl' : 'max-w-3xl'
	);

	async function load() {
		try {
			view = await publicQuota(key);
			error = '';
		} catch (e) {
			if (e instanceof ApiError && e.status === 403) {
				error = "This key doesn't have access to the quota page.";
				view = null;
			} else if (e instanceof ApiError && e.status === 401 && key) {
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
	});

	// Reload on the view's own schedule; 0 turns it off.
	const refreshSeconds = $derived(view?.refreshSeconds ?? 0);
	$effect(() => {
		if (refreshSeconds <= 0) return;
		const t = setInterval(load, refreshSeconds * 1000);
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

<main class="mx-auto min-h-svh {width} space-y-6 px-4 py-10 sm:px-6">
	<div class="flex flex-wrap items-end justify-between gap-3">
		<div>
			<h1 class="text-2xl font-semibold tracking-tight">{view?.title || meta?.title || 'Quota'}</h1>
			{#if view}<p class="text-sm text-muted-foreground">
					Updated {relativeTime(view.generatedAt)}
				</p>{/if}
		</div>
		<div class="flex items-center gap-2">
			{#if key}<Button variant="ghost" size="sm" onclick={forget}>Use a different key</Button>{/if}
			{#if view?.allowLayoutSwitch && !error}
				<ToggleGroup.Root
					type="single"
					variant="outline"
					size="sm"
					value={layout}
					onValueChange={(v) => v && layoutPref.set(v as QuotaLayout)}
				>
					<ToggleGroup.Item value="cards" class="px-2.5" aria-label="Cards" title="Cards">
						<LayoutGridIcon />
					</ToggleGroup.Item>
					<ToggleGroup.Item value="rows" class="px-2.5" aria-label="Rows" title="Rows">
						<Rows3Icon />
					</ToggleGroup.Item>
				</ToggleGroup.Root>
			{/if}
		</div>
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
		<QuotaViewComponent {view} {layout} />
	{/if}
</main>
