<script lang="ts">
	import type { Settings } from '#lib/api.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import ErrorRulesEditor from '#lib/components/error-rules-editor.svelte';
	import RedeemPolicyFields from '#lib/components/redeem-policy-fields.svelte';
	import UnsavedBar from '#lib/components/unsaved-bar.svelte';
	import { Draft } from '#lib/draft.svelte.js';
	import { saveSettings } from '#lib/settings.js';
	import { untrack } from 'svelte';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	const draft = new Draft<Settings>(untrack(() => data.settings));
	$effect.pre(() => draft.sync(data.settings));
	const form = $derived(draft.value);
	let saving = $state(false);

	async function save() {
		saving = true;
		await saveSettings(draft);
		saving = false;
	}
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-xl font-semibold tracking-tight">Account pool</h1>
		<p class="text-sm text-muted-foreground">Rules shared by every account.</p>
	</div>

	<form
		onsubmit={(e) => {
			e.preventDefault();
			save();
		}}
	>
		<div class="grid items-start gap-6 xl:grid-cols-2">
			<Card.Root>
				<Card.Header>
					<Card.Title>Requests</Card.Title>
				</Card.Header>
				<Card.Content>
					<Field.Group>
						<Field.Field>
							<Field.Label for="poll">Check account limits every (minutes)</Field.Label>
							<Input
								id="poll"
								type="number"
								min={1}
								class="w-32"
								bind:value={form.pool.quotaPollMinutes}
							/>
							<Field.Description
								>Limits also update live from every proxied response.</Field.Description
							>
						</Field.Field>
						<Field.Field>
							<Field.Label for="retries">Accounts to try per request</Field.Label>
							<Input
								id="retries"
								type="number"
								min={1}
								max={10}
								class="w-32"
								bind:value={form.pool.maxRetries}
							/>
							<Field.Description>
								When an account is rate limited or errors before answering, the request moves to the
								next one.
							</Field.Description>
						</Field.Field>
						<Field.Field>
							<Field.Label for="bootstrap"
								>Wait for the first output before committing (seconds)</Field.Label
							>
							<Input
								id="bootstrap"
								type="number"
								min={0}
								class="w-32"
								bind:value={form.pool.bootstrapSeconds}
							/>
							<Field.Description>
								A stream can still fail right after it starts, for example with an overload error.
								Holding the opening events back this long lets the request move to another account
								without the client noticing. 0 sends everything immediately.
							</Field.Description>
						</Field.Field>
						<Field.Field>
							<Field.Label for="keepalive">Keep idle streams alive every (seconds)</Field.Label>
							<Input
								id="keepalive"
								type="number"
								min={0}
								class="w-32"
								bind:value={form.pool.keepaliveSeconds}
							/>
							<Field.Description>
								Sends an empty comment while the model thinks, so proxies and clients with idle
								timeouts don't drop the connection. 0 turns it off.
							</Field.Description>
						</Field.Field>
					</Field.Group>
				</Card.Content>
			</Card.Root>

			<Card.Root>
				<Card.Header>
					<Card.Title>Banked resets</Card.Title>
					<Card.Description>
						When Codex accounts spend their banked resets on their own. Each account can override
						any of this in its Edit dialog. Claude accounts' banked resets are only spent by hand,
						from their cards.
					</Card.Description>
				</Card.Header>
				<Card.Content>
					<Field.Group>
						<Field.Field orientation="horizontal">
							<Field.Content>
								<Field.Label for="redeem-on">Use banked resets automatically</Field.Label>
								<Field.Description
									>The default for accounts that don't set their own.</Field.Description
								>
							</Field.Content>
							<Switch id="redeem-on" bind:checked={form.pool.redeem.enabled} />
						</Field.Field>
						<RedeemPolicyFields bind:policy={form.pool.redeem} idPrefix="pool-redeem" />
					</Field.Group>
				</Card.Content>
			</Card.Root>

			<Card.Root class="xl:col-span-2">
				<Card.Header>
					<Card.Title>Error handling</Card.Title>
					<Card.Description>
						Decide what happens when a provider returns an error: whether the request moves to the
						next account, and whether the account (or just the model on it) gets benched.
					</Card.Description>
				</Card.Header>
				<Card.Content>
					<ErrorRulesEditor bind:rules={form.pool.errorRules} />
				</Card.Content>
			</Card.Root>
		</div>
	</form>

	<UnsavedBar dirty={draft.dirty} {saving} onSave={save} onReset={() => draft.reset()} />
</div>
