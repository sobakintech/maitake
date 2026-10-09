<script lang="ts">
	import {
		admin,
		errorMessage,
		type ApiFormat,
		type KeyRules,
		type Route,
		type RoutingSettings
	} from '#lib/api.js';
	import { providerName } from '#lib/format.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import ProviderLogo from './provider-logo.svelte';
	import ArrowRightIcon from '@lucide/svelte/icons/arrow-right';
	import CircleAlertIcon from '@lucide/svelte/icons/circle-alert';

	// Previews where a request would go, using unsaved rules/settings when given.
	let {
		keyId,
		rules,
		allowed,
		routing,
		listId
	}: {
		keyId?: number;
		rules?: KeyRules;
		allowed?: string[];
		routing?: RoutingSettings;
		listId?: string;
	} = $props();

	const formats: { id: ApiFormat; label: string }[] = [
		{ id: 'anthropic', label: 'Anthropic (Claude Code)' },
		{ id: 'openai', label: 'OpenAI chat' },
		{ id: 'responses', label: 'OpenAI Responses' },
		{ id: 'openrouter', label: 'OpenRouter' }
	];
	const strategyName: Record<string, string> = {
		round_robin: 'take turns',
		fill: 'fill first',
		most_remaining: 'most quota left',
		random: 'random'
	};

	let model = $state('claude-haiku-4-5-20251001');
	let format = $state<ApiFormat>('anthropic');
	let result = $state<{ route: Route; error?: string } | null>(null);
	let failed = $state('');

	$effect(() => {
		const body = {
			model,
			format,
			keyId,
			rules: rules ? $state.snapshot(rules) : undefined,
			allowed: allowed ? $state.snapshot(allowed) : undefined,
			routing: routing ? $state.snapshot(routing) : undefined
		};
		if (!body.model.trim()) {
			result = null;
			return;
		}
		const timer = setTimeout(async () => {
			try {
				result = await admin.resolve(body as Parameters<typeof admin.resolve>[0]);
				failed = '';
			} catch (e) {
				failed = errorMessage(e);
			}
		}, 250);
		return () => clearTimeout(timer);
	});
</script>

<div class="space-y-3 rounded-lg border bg-muted/20 p-3">
	<div class="flex flex-wrap items-center gap-2">
		<Input
			class="w-64 font-mono text-xs"
			placeholder="Model a client sends"
			list={listId}
			bind:value={model}
		/>
		<Select.Root type="single" bind:value={format}>
			<Select.Trigger class="w-52">{formats.find((f) => f.id === format)?.label}</Select.Trigger>
			<Select.Content>
				{#each formats as f (f.id)}
					<Select.Item value={f.id} label={f.label}>{f.label}</Select.Item>
				{/each}
			</Select.Content>
		</Select.Root>
	</div>
	{#if failed}
		<p class="text-sm text-destructive">{failed}</p>
	{:else if result}
		{#if result.error}
			<p class="flex items-start gap-2 text-sm text-destructive">
				<CircleAlertIcon class="mt-0.5 size-4 shrink-0" />
				{result.error}
			</p>
		{:else}
			{@const r = result.route}
			<div class="space-y-1 text-sm">
				<p class="flex flex-wrap items-center gap-2">
					<code class="text-xs">{r.requested}</code>
					<ArrowRightIcon class="size-4 text-muted-foreground" />
					<ProviderLogo provider={r.model.provider} />
					<span class="font-medium">{r.model.name}</span>
					<code class="text-xs text-muted-foreground">{r.model.id}</code>
				</p>
				<p class="text-xs text-muted-foreground">
					{[
						r.mapped ? `rewritten by a ${r.rule} rule` : 'no mapping rule',
						`served by ${providerName[r.model.provider]}`,
						r.effort
							? `effort ${r.effort}`
							: r.defaultEffort
								? `effort ${r.defaultEffort} unless the request sets one`
								: 'effort from the request',
						`accounts: ${r.accounts?.length ? `${r.accounts.length} picked` : 'any'}, ${strategyName[r.strategy]}`,
						r.inCatalog ? '' : 'not in the catalog, passed through'
					]
						.filter(Boolean)
						.join(' · ')}
				</p>
			</div>
		{/if}
	{/if}
</div>
