<script lang="ts">
	import * as Card from '#lib/components/ui/card/index.js';
	import type { Snippet } from 'svelte';

	let { title, description, children }: { title: string; description: string; children: Snippet } =
		$props();
</script>

<main class="flex min-h-svh items-center justify-center bg-muted/40 p-6">
	<div class="w-full max-w-sm">
		<p class="mb-6 text-center text-lg font-semibold tracking-tight">maitake</p>
		<Card.Root>
			<Card.Header>
				<Card.Title>{title}</Card.Title>
				<Card.Description>{description}</Card.Description>
			</Card.Header>
			<Card.Content>
				{@render children()}
			</Card.Content>
		</Card.Root>
	</div>
</main>
