<script lang="ts">
	import type { Settings } from '#lib/api.js';
	import type { Provider } from '#lib/types.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import PrimerPolicyFields from '#lib/components/primer-policy-fields.svelte';
	import PingModelSelect from '#lib/components/ping-model-select.svelte';
	import ProviderLogo from '#lib/components/provider-logo.svelte';
	import UnsavedBar from '#lib/components/unsaved-bar.svelte';
	import { Draft } from '#lib/draft.svelte.js';
	import { providerName } from '#lib/format.js';
	import { defaultModels } from '#lib/primer.js';
	import { saveSettings } from '#lib/settings.js';
	import { untrack } from 'svelte';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	const draft = new Draft<Settings>(untrack(() => data.settings));
	$effect.pre(() => draft.sync(data.settings));
	const form = $derived(draft.value);

	const providers: Provider[] = ['claude', 'codex'];
	let saving = $state(false);

	async function save() {
		saving = true;
		await saveSettings(draft);
		saving = false;
	}
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-xl font-semibold tracking-tight">Session primer</h1>
		<p class="text-sm text-muted-foreground">
			Starts sessions ahead of time so limits reset when you want them to.
		</p>
	</div>

	<form
		class="grid items-start gap-6 xl:grid-cols-2"
		onsubmit={(e) => {
			e.preventDefault();
			save();
		}}
	>
		<Card.Root>
			<Card.Header>
				<Card.Title>Shared schedule</Card.Title>
				<Card.Description>
					Pick when limits should reset. Each session is started 5 hours earlier with a tiny
					request.
				</Card.Description>
			</Card.Header>
			<Card.Content>
				<PrimerPolicyFields
					bind:policy={form.pool.primer.schedule}
					serverTimezone={data.serverTimezone}
					idPrefix="primer"
				/>
			</Card.Content>
		</Card.Root>

		<div class="grid gap-6">
			<Card.Root>
				<Card.Header>
					<Card.Title>Ping</Card.Title>
				</Card.Header>
				<Card.Content>
					<Field.Group>
						{#each providers as p (p)}
							<Field.Field>
								<Field.Label for="primer-model-{p}" class="flex items-center gap-2">
									<ProviderLogo provider={p} />
									{providerName[p]} model
								</Field.Label>
								<PingModelSelect
									id="primer-model-{p}"
									class="sm:w-80"
									provider={p}
									catalog={data.models}
									bind:value={
										() => form.pool.primer.models[p] || defaultModels[p],
										(v) => (form.pool.primer.models[p] = v)
									}
								/>
							</Field.Field>
						{/each}
						<Field.Field>
							<Field.Label for="primer-prompt">Prompt</Field.Label>
							<Input
								id="primer-prompt"
								class="sm:w-80"
								maxlength={200}
								bind:value={form.pool.primer.prompt}
							/>
						</Field.Field>
					</Field.Group>
				</Card.Content>
			</Card.Root>
		</div>
	</form>

	<UnsavedBar dirty={draft.dirty} {saving} onSave={save} onReset={() => draft.reset()} />
</div>
