<script lang="ts">
	import type { ApiFormat, CatalogModel, MappingRule } from '#lib/types.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import ArrowDownIcon from '@lucide/svelte/icons/arrow-down';
	import ArrowRightIcon from '@lucide/svelte/icons/arrow-right';
	import ArrowUpIcon from '@lucide/svelte/icons/arrow-up';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';

	let {
		rules = $bindable(),
		catalog,
		idPrefix = 'rules'
	}: { rules: MappingRule[]; catalog: CatalogModel[]; idPrefix?: string } = $props();

	const efforts = [
		{ value: '', label: 'Keep requested effort' },
		{ value: 'none', label: 'Effort: none' },
		{ value: 'minimal', label: 'Effort: minimal' },
		{ value: 'low', label: 'Effort: low' },
		{ value: 'medium', label: 'Effort: medium' },
		{ value: 'high', label: 'Effort: high' },
		{ value: 'xhigh', label: 'Effort: xhigh' },
		{ value: 'max', label: 'Effort: max' }
	];

	const formats: { id: ApiFormat; label: string }[] = [
		{ id: 'anthropic', label: 'Anthropic' },
		{ id: 'openai', label: 'OpenAI chat' },
		{ id: 'responses', label: 'Responses' },
		{ id: 'openrouter', label: 'OpenRouter' }
	];

	const suggestions = $derived([
		...new Set([
			'claude-haiku-*',
			'claude-sonnet-*',
			'claude-opus-*',
			'gpt-*',
			'anthropic/*',
			'openai/*',
			...catalog.flatMap((m) => [m.id, m.canonical])
		])
	]);

	function add() {
		rules = [
			...rules,
			{
				id: crypto.randomUUID(),
				enabled: true,
				match: '',
				target: '',
				effort: '',
				formats: [],
				note: ''
			}
		];
	}

	function move(i: number, d: number) {
		const next = [...rules];
		[next[i], next[i + d]] = [next[i + d], next[i]];
		rules = next;
	}

	function toggleFormat(rule: MappingRule, f: ApiFormat) {
		const cur = rule.formats ?? [];
		rule.formats = cur.includes(f) ? cur.filter((x) => x !== f) : [...cur, f];
	}
</script>

<datalist id="{idPrefix}-models">
	{#each suggestions as s (s)}<option value={s}></option>{/each}
</datalist>

<div class="space-y-3">
	{#each rules as rule, i (rule.id)}
		<div class="space-y-3 rounded-lg border p-3 {rule.enabled ? '' : 'opacity-60'}">
			<div class="flex flex-wrap items-center gap-2">
				<Switch bind:checked={rule.enabled} aria-label="Rule enabled" />
				<Input
					class="w-48 font-mono text-xs"
					placeholder="claude-haiku-*"
					list="{idPrefix}-models"
					aria-label="Requested model pattern"
					bind:value={rule.match}
				/>
				<ArrowRightIcon class="size-4 shrink-0 text-muted-foreground" />
				<Input
					class="w-48 font-mono text-xs"
					placeholder="gpt-5.5"
					list="{idPrefix}-models"
					aria-label="Use this model instead"
					bind:value={rule.target}
				/>
				<Select.Root type="single" bind:value={rule.effort}>
					<Select.Trigger class="w-48">
						{efforts.find((e) => e.value === rule.effort)?.label ?? rule.effort}
					</Select.Trigger>
					<Select.Content>
						{#each efforts as e (e.value)}
							<Select.Item value={e.value} label={e.label}>{e.label}</Select.Item>
						{/each}
					</Select.Content>
				</Select.Root>
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
				<span class="text-xs text-muted-foreground">Applies to</span>
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
			Rules are checked top to bottom; the first match wins. Patterns use <code>*</code> and match names
			with or without a lab prefix.
		</p>
		<Button size="sm" variant="outline" onclick={add}><PlusIcon /> Add rule</Button>
	</div>
</div>
