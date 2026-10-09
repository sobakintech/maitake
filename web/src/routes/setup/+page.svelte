<script lang="ts">
	import { goto } from '$app/navigation';
	import { api, errorMessage } from '#lib/api.js';
	import AuthCard from '#lib/components/auth-card.svelte';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';

	let token = $state('');
	let username = $state('admin');
	let password = $state('');
	let confirm = $state('');
	let error = $state('');
	let busy = $state(false);

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		error = '';
		if (password !== confirm) {
			error = 'Passwords do not match';
			return;
		}
		busy = true;
		try {
			await api.setup(token.trim(), username, password);
			await goto('/overview', { invalidateAll: true });
		} catch (err) {
			error = errorMessage(err);
		} finally {
			busy = false;
		}
	}
</script>

<AuthCard
	title="Create admin account"
	description="First run. Paste the setup token from the server log."
>
	<form onsubmit={submit}>
		<Field.Group>
			<Field.Field>
				<Field.Label for="token">Setup token</Field.Label>
				<Input id="token" autocomplete="off" required bind:value={token} class="font-mono" />
			</Field.Field>
			<Field.Field>
				<Field.Label for="username">Username</Field.Label>
				<Input id="username" autocomplete="username" required bind:value={username} />
			</Field.Field>
			<Field.Field>
				<Field.Label for="password">Password</Field.Label>
				<Input
					id="password"
					type="password"
					autocomplete="new-password"
					minlength={10}
					required
					bind:value={password}
				/>
				<Field.Description>At least 10 characters.</Field.Description>
			</Field.Field>
			<Field.Field data-invalid={!!error}>
				<Field.Label for="confirm">Confirm password</Field.Label>
				<Input
					id="confirm"
					type="password"
					autocomplete="new-password"
					required
					bind:value={confirm}
				/>
				{#if error}<Field.Error>{error}</Field.Error>{/if}
			</Field.Field>
			<Button type="submit" disabled={busy}>Create account</Button>
		</Field.Group>
	</form>
</AuthCard>
