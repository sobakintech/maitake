<script lang="ts">
	import { goto } from '$app/navigation';
	import { admin, errorMessage, type DisplayProfile } from '#lib/api.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as Select from '#lib/components/ui/select/index.js';

	let { open = $bindable(false), profiles }: { open: boolean; profiles: DisplayProfile[] } =
		$props();

	let name = $state('');
	let from = $state('defaults');
	let error = $state('');
	let busy = $state(false);

	$effect(() => {
		if (open) {
			name = '';
			from = 'defaults';
			error = '';
		}
	});

	const fromLabel = $derived(
		from === 'defaults'
			? 'Default settings'
			: `Copy of ${profiles.find((p) => String(p.id) === from)?.name ?? '…'}`
	);

	async function create(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		error = '';
		try {
			const source = profiles.find((p) => String(p.id) === from);
			const config = source ? structuredClone(source.config) : (await admin.profiles()).defaults;
			const { profile } = await admin.createProfile(name, config);
			open = false;
			await goto(`/quota-page/profiles/${profile.id}`, { invalidateAll: true });
		} catch (err) {
			error = errorMessage(err);
		} finally {
			busy = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-md">
		<form onsubmit={create} class="space-y-6">
			<Dialog.Header>
				<Dialog.Title>New display profile</Dialog.Title>
				<Dialog.Description>You'll choose exactly what it shows next.</Dialog.Description>
			</Dialog.Header>
			<Field.Group>
				<Field.Field>
					<Field.Label for="np-name">Name</Field.Label>
					<Input
						id="np-name"
						required
						maxlength={100}
						placeholder="e.g. Friends"
						bind:value={name}
					/>
				</Field.Field>
				<Field.Field data-invalid={!!error}>
					<Field.Label>Start from</Field.Label>
					<Select.Root type="single" bind:value={from}>
						<Select.Trigger class="w-full">{fromLabel}</Select.Trigger>
						<Select.Content>
							<Select.Item value="defaults" label="Default settings">Default settings</Select.Item>
							{#each profiles as p (p.id)}
								<Select.Item value={String(p.id)} label={`Copy of ${p.name}`}>
									Copy of {p.name}
								</Select.Item>
							{/each}
						</Select.Content>
					</Select.Root>
					{#if error}<Field.Error>{error}</Field.Error>{/if}
				</Field.Field>
			</Field.Group>
			<Dialog.Footer>
				<Button type="submit" disabled={busy || !name.trim()}>Create and edit</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
