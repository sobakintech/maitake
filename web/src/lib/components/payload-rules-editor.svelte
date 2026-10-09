<script lang="ts">
	import type { ApiFormat, CatalogModel, PayloadRule } from '#lib/types.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import ArrowDownIcon from '@lucide/svelte/icons/arrow-down';
	import ArrowUpIcon from '@lucide/svelte/icons/arrow-up';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';

	let { rules = $bindable(), catalog }: { rules: PayloadRule[]; catalog: CatalogModel[] } =
		$props();

	const providers = [
		{ value: '', label: 'Any provider' },
		{ value: 'claude', label: 'Claude' },
		{ value: 'codex', label: 'Codex' }
	];
	const modes = [
		{ value: 'default', label: 'Set if missing' },
		{ value: 'override', label: 'Always set' },
		{ value: 'remove', label: 'Remove' }
	];
	const formats: { id: ApiFormat; label: string }[] = [
		{ id: 'anthropic', label: 'Anthropic' },
		{ id: 'openai', label: 'OpenAI chat' },
		{ id: 'responses', label: 'Responses' },
		{ id: 'openrouter', label: 'OpenRouter' }
	];

	const suggestions = $derived([
		...new Set(['claude-*', 'gpt-*', ...catalog.flatMap((m) => [m.id, m.canonical])])
	]);

	function label(list: { value: string; label: string }[], v: string) {
		return list.find((x) => x.value === v)?.label ?? v;
	}

	const valuePlaceholder = '"high"';

	function shown(v: unknown) {
		return v === undefined || v === null ? '' : JSON.stringify(v);
	}

	// Values are JSON; anything that doesn't parse is taken as a plain string.
	function setValue(rule: PayloadRule, text: string) {
		try {
			rule.value = JSON.parse(text);
		} catch {
			rule.value = text;
		}
	}

	function add() {
		rules = [
			...rules,
			{
				id: crypto.randomUUID(),
				enabled: true,
				match: '',
				provider: '',
				formats: [],
				mode: 'override',
				path: '',
				value: '',
				note: ''
			}
		];
	}

	function move(i: number, d: number) {
		const next = [...rules];
		[next[i], next[i + d]] = [next[i + d], next[i]];
		rules = next;
	}

	function toggleFormat(rule: PayloadRule, f: ApiFormat) {
		const cur = rule.formats ?? [];
		rule.formats = cur.includes(f) ? cur.filter((x) => x !== f) : [...cur, f];
	}
</script>

<datalist id="payload-models">
	{#each suggestions as s (s)}<option value={s}></option>{/each}
</datalist>

<div class="space-y-3">
	{#each rules as rule, i (rule.id)}
		<div class="space-y-3 rounded-lg border p-3 {rule.enabled ? '' : 'opacity-60'}">
			<div class="flex flex-wrap items-center gap-2">
				<Switch bind:checked={rule.enabled} aria-label="Rule enabled" />
				<Select.Root type="single" bind:value={rule.mode}>
					<Select.Trigger class="w-36">{label(modes, rule.mode)}</Select.Trigger>
					<Select.Content>
						{#each modes as m (m.value)}
							<Select.Item value={m.value} label={m.label}>{m.label}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
				<Input
					class="w-48 font-mono text-xs"
					placeholder="reasoning.effort"
					aria-label="Field path"
					bind:value={rule.path}
				/>
				{#if rule.mode !== 'remove'}
					<span class="text-xs text-muted-foreground">to</span>
					<Input
						class="min-w-32 flex-1 font-mono text-xs"
						placeholder={valuePlaceholder}
						aria-label="Value (JSON)"
						value={shown(rule.value)}
						onchange={(e) => setValue(rule, e.currentTarget.value)}
					/>
				{/if}
				<div class="ml-auto flex">
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
				<span class="text-xs text-muted-foreground">When</span>
				<Input
					class="h-7 w-44 font-mono text-xs"
					placeholder="any model"
					list="payload-models"
					aria-label="Model pattern"
					bind:value={rule.match}
				/>
				<Select.Root type="single" bind:value={rule.provider}>
					<Select.Trigger size="sm" class="w-36">{label(providers, rule.provider)}</Select.Trigger>
					<Select.Content>
						{#each providers as p (p.value)}
							<Select.Item value={p.value} label={p.label}>{p.label}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
				<Button
					size="xs"
					variant={(rule.formats ?? []).length === 0 ? 'default' : 'outline'}
					onclick={() => (rule.formats = [])}>All APIs</Button
				>
				{#each formats as f (f.id)}
					<Button
						size="xs"
						variant={(rule.formats ?? []).includes(f.id) ? 'default' : 'outline'}
						onclick={() => toggleFormat(rule, f.id)}>{f.label}</Button
					>
				{/each}
				<Input
					class="h-7 min-w-40 flex-1 text-xs"
					placeholder="Note (optional)"
					bind:value={rule.note}
				/>
			</div>
		</div>
	{:else}
		<p class="rounded-lg border border-dashed py-6 text-center text-sm text-muted-foreground">
			No rules yet.
		</p>
	{/each}
	<div class="flex flex-wrap items-center justify-between gap-2">
		<p class="text-xs text-muted-foreground">
			Edits the request after it's translated for the provider, right before it's sent. Paths use
			dots (<code>reasoning.effort</code>, <code>tools.0.name</code>); values are JSON, so text
			needs quotes. All matching rules apply, top to bottom.
		</p>
		<Button size="sm" variant="outline" onclick={add}><PlusIcon /> Add rule</Button>
	</div>
</div>
