<script lang="ts">
	import { goto } from '$app/navigation';
	import { api, errorMessage } from '#lib/api.js';
	import AuthCard from '#lib/components/auth-card.svelte';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';

	let username = $state('');
	let password = $state('');
	let error = $state('');
	let busy = $state(false);

	async function submit(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		error = '';
		try {
			await api.login(username, password);
			await goto('/overview', { invalidateAll: true });
		} catch (err) {
			error = errorMessage(err);
		} finally {
			busy = false;
		}
	}
</script>

<AuthCard title="Sign in" description="Admin access to the maitake dashboard.">
	<form onsubmit={submit}>
		<Field.Group>
			<Field.Field>
				<Field.Label for="username">Username</Field.Label>
				<Input id="username" autocomplete="username" required bind:value={username} />
			</Field.Field>
			<Field.Field data-invalid={!!error}>
				<Field.Label for="password">Password</Field.Label>
				<Input
					id="password"
					type="password"
					autocomplete="current-password"
					required
					bind:value={password}
				/>
				{#if error}<Field.Error>{error}</Field.Error>{/if}
			</Field.Field>
			<Button type="submit" disabled={busy}>Sign in</Button>
		</Field.Group>
	</form>
</AuthCard>
