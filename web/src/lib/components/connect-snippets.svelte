<script lang="ts">
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import ProviderLogo from './provider-logo.svelte';
	import CheckIcon from '@lucide/svelte/icons/check';
	import CopyIcon from '@lucide/svelte/icons/copy';

	// apiKey fills the snippets with a real key (right after creating one); otherwise a placeholder.
	let { apiKey }: { apiKey?: string } = $props();

	const origin = typeof window === 'undefined' ? '' : window.location.origin;
	const key = $derived(apiKey ?? 'mtk_…');
	let copied = $state('');

	const snippets = $derived({
		claude: `export ANTHROPIC_BASE_URL=${origin}\nexport ANTHROPIC_AUTH_TOKEN=${key}`,
		codex: `# ~/.codex/config.toml\nmodel_provider = "maitake"\n\n[model_providers.maitake]\nname = "maitake"\nbase_url = "${origin}/v1"\nwire_api = "responses"\nenv_key = "MAITAKE_API_KEY"\n\n# then: export MAITAKE_API_KEY=${key}`,
		openai: `Base URL: ${origin}/v1\nAPI key:  ${key}`
	});

	async function copy(id: keyof typeof snippets) {
		await navigator.clipboard.writeText(snippets[id]);
		copied = id;
		setTimeout(() => copied === id && (copied = ''), 1500);
	}
</script>

{#snippet block(id: keyof typeof snippets)}
	<div class="relative">
		<pre
			class="overflow-x-auto rounded-lg bg-muted px-3 py-2 pr-10 font-mono text-xs leading-relaxed">{snippets[
				id
			]}</pre>
		<Button
			variant="ghost"
			size="icon-sm"
			class="absolute top-1 right-1"
			aria-label="Copy"
			onclick={() => copy(id)}
		>
			{#if copied === id}<CheckIcon />{:else}<CopyIcon />{/if}
		</Button>
	</div>
{/snippet}

<Tabs.Root value="claude">
	<Tabs.List>
		<Tabs.Trigger value="claude" class="gap-1.5"
			><ProviderLogo provider="claude" /> Claude Code</Tabs.Trigger
		>
		<Tabs.Trigger value="codex" class="gap-1.5"
			><ProviderLogo provider="codex" /> Codex CLI</Tabs.Trigger
		>
		<Tabs.Trigger value="openai">Other apps</Tabs.Trigger>
	</Tabs.List>
	<Tabs.Content value="claude" class="space-y-2 pt-3 text-sm">
		{@render block('claude')}
		<p class="text-xs text-muted-foreground">
			Works with Claude models and Codex models (e.g. <code>ANTHROPIC_MODEL=gpt-5.5</code>).
		</p>
	</Tabs.Content>
	<Tabs.Content value="codex" class="space-y-2 pt-3 text-sm">
		{@render block('codex')}
		<p class="text-xs text-muted-foreground">
			Codex's model picker lists every model the key can use, Claude models included (e.g.
			<code>model = "claude-opus-5"</code>).
		</p>
	</Tabs.Content>
	<Tabs.Content value="openai" class="space-y-2 pt-3 text-sm">
		{@render block('openai')}
		<p class="text-xs text-muted-foreground">
			For anything that speaks the OpenAI or Anthropic API. Add a reasoning level to a model name,
			e.g. <code>gpt-5.5(high)</code> or <code>claude-opus-5(max)</code>.
		</p>
	</Tabs.Content>
</Tabs.Root>
