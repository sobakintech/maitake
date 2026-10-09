<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import { admin, errorMessage, type ProfileConfig } from '#lib/api.js';
	import { Draft } from '#lib/draft.svelte.js';
	import DisplayConfigEditor from '#lib/components/display-config-editor.svelte';
	import UnsavedBar from '#lib/components/unsaved-bar.svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	type Form = { name: string; config: ProfileConfig };
	const toForm = (): Form => ({ name: data.profile.name, config: data.profile.config });

	const draft = new Draft<Form>(untrack(toForm));
	let loadedFor = untrack(() => data.profile.id);
	$effect.pre(() => {
		const next = toForm();
		// Switching to another profile discards the draft; a refresh of the same one keeps edits.
		if (data.profile.id !== loadedFor) {
			loadedFor = data.profile.id;
			draft.base = next;
			draft.reset();
		} else {
			draft.sync(next);
		}
	});
	const form = $derived(draft.value);

	let saving = $state(false);
	async function save() {
		if (!form.name.trim()) {
			toast.error('The profile needs a name');
			return;
		}
		saving = true;
		try {
			const next = draft.snapshot();
			const { profile } = await admin.updateProfile(data.profile.id, next);
			draft.value = { name: profile.name, config: profile.config };
			draft.commit();
			toast.success('Profile saved');
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
		<h1 class="text-xl font-semibold tracking-tight">{data.profile.name}</h1>
	</div>

	<DisplayConfigEditor bind:config={form.config} accounts={data.accounts}>
		{#snippet top()}
			<Card.Root>
				<Card.Content>
					<Field.Field>
						<Field.Label for="p-name">Profile name</Field.Label>
						<Input id="p-name" maxlength={100} placeholder="e.g. Friends" bind:value={form.name} />
					</Field.Field>
				</Card.Content>
			</Card.Root>
		{/snippet}
	</DisplayConfigEditor>

	<UnsavedBar dirty={draft.dirty} {saving} onSave={save} onReset={() => draft.reset()} />
</div>
