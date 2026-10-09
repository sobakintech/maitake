<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import { api, errorMessage, type ProfileConfig } from '#lib/api.js';
	import { Draft } from '#lib/draft.svelte.js';
	import DisplayConfigEditor from '#lib/components/display-config-editor.svelte';
	import UnsavedBar from '#lib/components/unsaved-bar.svelte';
	import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	// A key without its own view starts from the standard settings.
	const initial = () => data.key.displayConfig ?? data.blankView;
	const draft = new Draft<ProfileConfig>(untrack(initial));
	$effect.pre(() => draft.sync(initial()));
	let saving = $state(false);
	const isNew = $derived(!data.key.displayConfig);

	async function save() {
		saving = true;
		try {
			const { key } = await api.updateKey(data.key.id, { displayConfig: draft.snapshot() });
			draft.value = structuredClone(key.displayConfig!);
			draft.commit();
			toast.success('Saved');
			await invalidateAll();
		} catch (e) {
			toast.error(errorMessage(e));
		} finally {
			saving = false;
		}
	}
</script>

<div class="mx-auto max-w-6xl space-y-6">
	<div class="space-y-2">
		<a
			href="/keys/{data.key.id}"
			class="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
		>
			<ArrowLeftIcon class="size-4" />
			{data.key.name || data.key.prefix}
		</a>
		<h1 class="text-xl font-semibold tracking-tight">Quota page view for this key</h1>
		<p class="text-sm text-muted-foreground">
			Only this key sees it, whatever profile is assigned.
			{#if isNew}Starts from the standard settings; save to use it.{/if}
		</p>
	</div>

	<DisplayConfigEditor bind:config={draft.value} accounts={data.accounts} />

	<UnsavedBar dirty={draft.dirty || isNew} {saving} onSave={save} onReset={() => draft.reset()} />
</div>
