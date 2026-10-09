<script lang="ts">
	import type { ErrorRule } from '#lib/types.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import ArrowDownIcon from '@lucide/svelte/icons/arrow-down';
	import ArrowUpIcon from '@lucide/svelte/icons/arrow-up';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';

	let { rules = $bindable() }: { rules: ErrorRule[] } = $props();

	const providers = [
		{ value: '', label: 'Any provider' },
		{ value: 'claude', label: 'Claude' },
		{ value: 'codex', label: 'Codex' }
	];
	const actions = [
		{ value: 'retry', label: 'Try the next account' },
		{ value: 'stop', label: 'Return the error' }
	];
	const cooldowns = [
		{ value: '', label: 'Bench: automatic' },
		{ value: 'none', label: 'Bench: nothing' },
		{ value: 'account', label: 'Bench: the account' },
		{ value: 'model', label: 'Bench: this model only' }
	];

	function label(list: { value: string; label: string }[], v: string) {
		return list.find((x) => x.value === v)?.label ?? v;
	}

	function add() {
		rules = [
			...rules,
			{
				id: crypto.randomUUID(),
				enabled: true,
				provider: '',
				status: 0,
				match: '',
				action: 'retry',
				cooldown: '',
				cooldownSeconds: 0,
				note: ''
			}
		];
	}

	function move(i: number, d: number) {
		const next = [...rules];
		[next[i], next[i + d]] = [next[i + d], next[i]];
		rules = next;
	}
</script>

<div class="space-y-3">
	{#each rules as rule, i (rule.id)}
		<div class="space-y-3 rounded-lg border p-3 {rule.enabled ? '' : 'opacity-60'}">
			<div class="flex flex-wrap items-center gap-2">
				<Switch bind:checked={rule.enabled} aria-label="Rule enabled" />
				<Select.Root type="single" bind:value={rule.provider}>
					<Select.Trigger class="w-36">{label(providers, rule.provider)}</Select.Trigger>
					<Select.Content>
						{#each providers as p (p.value)}
							<Select.Item value={p.value} label={p.label}>{p.label}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
				<Input
					class="w-24 font-mono text-xs"
					type="number"
					min={0}
					placeholder="Status"
					aria-label="HTTP status, 0 for any"
					bind:value={rule.status}
				/>
				<Input
					class="min-w-48 flex-1 font-mono text-xs"
					placeholder="text in the error, or /regex/"
					aria-label="Error text to match"
					bind:value={rule.match}
				/>
				<div class="flex">
					<Button
						variant="ghost"
						size="icon-sm"
						aria-label="Move up"
						disabled={i === 0}
						onclick={() => move(i, -1)}><ArrowUpIcon /></Button
					>
					<Button
						variant="ghost"
						size="icon-sm"
						aria-label="Move down"
						disabled={i === rules.length - 1}
						onclick={() => move(i, 1)}><ArrowDownIcon /></Button
					>
					<Button
						variant="ghost"
						size="icon-sm"
						aria-label="Delete rule"
						onclick={() => (rules = rules.filter((r) => r.id !== rule.id))}><Trash2Icon /></Button
					>
				</div>
			</div>
			<div class="flex flex-wrap items-center gap-2">
				<Select.Root type="single" bind:value={rule.action}>
					<Select.Trigger class="w-48">{label(actions, rule.action)}</Select.Trigger>
					<Select.Content>
						{#each actions as a (a.value)}
							<Select.Item value={a.value} label={a.label}>{a.label}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
				<Select.Root type="single" bind:value={rule.cooldown}>
					<Select.Trigger class="w-52">{label(cooldowns, rule.cooldown)}</Select.Trigger>
					<Select.Content>
						{#each cooldowns as c (c.value)}
							<Select.Item value={c.value} label={c.label}>{c.label}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
				{#if rule.cooldown !== 'none'}
					<div class="flex items-center gap-1.5">
						<span class="text-xs text-muted-foreground">for</span>
						<Input
							class="h-8 w-20 text-xs"
							type="number"
							min={0}
							aria-label="Bench seconds, 0 for automatic"
							bind:value={rule.cooldownSeconds}
						/>
						<span class="text-xs text-muted-foreground">s</span>
					</div>
				{/if}
				<Input
					class="h-8 min-w-40 flex-1 text-xs"
					placeholder="Note (optional)"
					bind:value={rule.note}
				/>
			</div>
		</div>
	{:else}
		<p class="rounded-lg border border-dashed py-6 text-center text-sm text-muted-foreground">
			No rules. Every error uses the built-in handling.
		</p>
	{/each}
	<div class="flex flex-wrap items-center justify-between gap-2">
		<p class="text-xs text-muted-foreground">
			Checked top to bottom; the first match wins. Status 0 and an empty match mean any. Benching
			for 0 seconds uses the time the provider reports, if any.
		</p>
		<Button size="sm" variant="outline" onclick={add}><PlusIcon /> Add rule</Button>
	</div>
</div>
