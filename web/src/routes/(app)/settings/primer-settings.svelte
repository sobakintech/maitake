<script lang="ts">
	import type { CatalogModel, PrimerSettings } from '#lib/types.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import PrimerPolicyFields from '#lib/components/primer-policy-fields.svelte';
	import PingModelSelect from '#lib/components/ping-model-select.svelte';
	import ProviderLogo from '#lib/components/provider-logo.svelte';
	import { defaultModels } from '#lib/primer.js';

	let {
		primer = $bindable(),
		serverTimezone,
		catalog
	}: { primer: PrimerSettings; serverTimezone: string; catalog: CatalogModel[] } = $props();
</script>

<div class="grid items-start gap-6 xl:grid-cols-2">
	<Card.Root>
		<Card.Header>
			<Card.Title>Shared schedule</Card.Title>
			<Card.Description>
				Pick when limits should reset. Each session is started 5 hours earlier with a tiny request.
				Turn it on per account on the Accounts page.
			</Card.Description>
		</Card.Header>
		<Card.Content>
			<PrimerPolicyFields bind:policy={primer.schedule} {serverTimezone} idPrefix="primer" />
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header>
			<Card.Title>Ping</Card.Title>
		</Card.Header>
		<Card.Content>
			<Field.Group>
				{#each ['claude', 'codex'] as const as p (p)}
					<Field.Field>
						<Field.Label for="primer-model-{p}" class="flex items-center gap-2">
							<ProviderLogo provider={p} />
							{p === 'claude' ? 'Claude' : 'Codex'} model
						</Field.Label>
						<PingModelSelect
							id="primer-model-{p}"
							class="sm:w-80"
							provider={p}
							{catalog}
							bind:value={() => primer.models[p] || defaultModels[p], (v) => (primer.models[p] = v)}
						/>
					</Field.Field>
				{/each}
				<Field.Field>
					<Field.Label for="primer-prompt">Prompt</Field.Label>
					<Input id="primer-prompt" class="sm:w-80" maxlength={200} bind:value={primer.prompt} />
				</Field.Field>
			</Field.Group>
		</Card.Content>
	</Card.Root>
</div>
