<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import { admin, errorMessage, type ProfileConfig } from '#lib/api.js';
	import { Draft } from '#lib/draft.svelte.js';
	import DisplayConfigEditor from '#lib/components/display-config-editor.svelte';
	import UnsavedBar from '#lib/components/unsaved-bar.svelte';
	import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	const draft = new Draft<ProfileConfig>(untrack(() => data.settings.quotaPage.defaultView));
	$effect.pre(() => draft.sync(data.settings.quotaPage.defaultView));
	let saving = $state(false);

	async function save() {
		saving = true;
		try {
			const { settings } = await admin.saveSettings({
				...data.settings,
				quotaPage: { ...data.settings.quotaPage, defaultView: draft.snapshot() }
			});
			draft.value = structuredClone(settings.quotaPage.defaultView);
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
			href="/quota-page"
			class="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
		>
			<ArrowLeftIcon class="size-4" /> Quota page
		</a>
		<h1 class="text-xl font-semibold tracking-tight">Default view</h1>
		<p class="text-sm text-muted-foreground">
			What keys see when they have no view or profile of their own.
			{#if data.settings.quotaPage.defaultProfileId}
				A profile is currently set as the default, so this is only used if you switch back.
			{/if}
		</p>
	</div>

	<DisplayConfigEditor bind:config={draft.value} accounts={data.accounts} />

	<UnsavedBar dirty={draft.dirty} {saving} onSave={save} onReset={() => draft.reset()} />
</div>
