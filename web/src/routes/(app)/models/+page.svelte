<script lang="ts">
	import { goto, invalidateAll } from '$app/navigation';
	import { page } from '$app/state';
	import {
		admin,
		errorMessage,
		type CatalogModel,
		type CustomModel,
		type ModelOverride,
		type Provider,
		type Settings,
		type Strategy
	} from '#lib/api.js';
	import { compact, providerName } from '#lib/format.js';
	import { Draft } from '#lib/draft.svelte.js';
	import LabLogo from '#lib/components/lab-logo.svelte';
	import ProviderLogo from '#lib/components/provider-logo.svelte';
	import RouteTester from '#lib/components/route-tester.svelte';
	import RulesEditor from '#lib/components/rules-editor.svelte';
	import PayloadRulesEditor from '#lib/components/payload-rules-editor.svelte';
	import UnsavedBar from '#lib/components/unsaved-bar.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as RadioGroup from '#lib/components/ui/radio-group/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import Settings2Icon from '@lucide/svelte/icons/settings-2';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import CustomModelDialog from './custom-model-dialog.svelte';
	import ModelDialog from './model-dialog.svelte';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	const draft = new Draft<Settings>(untrack(() => data.settings));
	$effect.pre(() => draft.sync(data.settings));
	const rs = $derived(draft.value.routing);
	let saving = $state(false);

	const tabs = [
		{ id: 'catalog', label: 'Models' },
		{ id: 'mapping', label: 'Model mapping' },
		{ id: 'routing', label: 'Routing' },
		{ id: 'requests', label: 'Request rules' },
		{ id: 'images', label: 'Images' }
	];

	const affinityOptions = [
		{
			value: 'session',
			label: 'Per conversation',
			hint: 'Each conversation stays on one account. Keeps prompt caches warm and reasoning valid while spreading different conversations across accounts.'
		},
		{
			value: 'key',
			label: 'Per key',
			hint: 'Everything from one key stays on one account.'
		},
		{ value: 'off', label: 'Off', hint: 'Every request picks an account from scratch.' }
	];
	const tab = $derived(
		tabs.some((t) => t.id === page.url.searchParams.get('tab'))
			? page.url.searchParams.get('tab')!
			: 'catalog'
	);
	function selectTab(v: string) {
		const url = new URL(page.url.href);
		url.searchParams.set('tab', v);
		goto(url, { replace: true, reset: false });
	}

	const labFor: Record<Provider, string> = { claude: 'anthropic', codex: 'openai' };
	const emptyOverride: ModelOverride = {
		disabled: false,
		name: '',
		accounts: [],
		strategy: '',
		effort: ''
	};

	// The catalog as it will be after saving: built-ins from the server plus the draft's custom models.
	const catalog = $derived<CatalogModel[]>([
		...data.models.filter((m) => !m.custom),
		...rs.custom.map((c) => {
			const lab = labFor[c.provider];
			return {
				id: c.id,
				canonical: `${lab}/${c.id}`,
				lab,
				provider: c.provider,
				name: c.name || c.id,
				contextLength: c.contextLength,
				maxOutput: 0,
				custom: true,
				disabled: false,
				override: emptyOverride,
				strategy: rs.strategy,
				accounts: 0
			};
		})
	]);

	function ov(canonical: string): ModelOverride {
		return { ...emptyOverride, ...(rs.models[canonical] ?? {}) };
	}

	function setOv(canonical: string, o: ModelOverride) {
		const isDefault =
			!o.disabled && !o.name && !o.effort && !o.strategy && !(o.accounts ?? []).length;
		const next = { ...rs.models };
		if (isDefault) delete next[canonical];
		else next[canonical] = o;
		rs.models = next;
	}

	function servingAccounts(m: CatalogModel) {
		const o = ov(m.canonical);
		return data.accounts.filter(
			(a) =>
				a.provider === m.provider &&
				!a.disabled &&
				(!(o.accounts ?? []).length || o.accounts!.includes(a.id))
		).length;
	}

	const strategyLabel: Record<string, string> = {
		round_robin: 'Take turns',
		fill: 'Fill first',
		most_remaining: 'Most quota left',
		random: 'Random'
	};
	const strategyOptions: { value: Strategy; label: string; hint: string }[] = [
		{
			value: 'round_robin',
			label: 'Take turns',
			hint: 'Rotate through accounts so usage spreads evenly.'
		},
		{
			value: 'fill',
			label: 'Fill one account first',
			hint: 'Use the same account until it hits a limit, then move on. Keeps the others fresh.'
		},
		{
			value: 'most_remaining',
			label: 'Most quota left',
			hint: 'Pick the account with the most of its 5-hour/weekly limit remaining.'
		},
		{ value: 'random', label: 'Random', hint: 'Pick any available account.' }
	];

	let editing = $state<CatalogModel | null>(null);
	let editOpen = $state(false);
	let customOpen = $state(false);

	function addCustom(m: CustomModel) {
		rs.custom = [...rs.custom, m];
	}

	function removeCustom(m: CatalogModel) {
		rs.custom = rs.custom.filter((c) => !(c.id === m.id && c.provider === m.provider));
		setOv(m.canonical, emptyOverride);
	}

	async function save() {
		saving = true;
		try {
			const next = draft.snapshot();
			next.routing.stickyMinutes = Number(next.routing.stickyMinutes);
			next.routing.payloadRules = next.routing.payloadRules.filter((r) => r.path.trim() !== '');
			const { settings } = await admin.saveSettings(next);
			draft.value = structuredClone(settings);
			draft.commit();
			toast.success('Saved');
			await invalidateAll();
		} catch (e) {
			toast.error(errorMessage(e));
		} finally {
			saving = false;
		}
	}
</script>

<datalist id="catalog-models">
	{#each catalog as m (m.canonical)}
		<option value={m.id}></option>
		<option value={m.canonical}></option>
	{/each}
</datalist>

<div class="space-y-6">
	<div>
		<h1 class="text-xl font-semibold tracking-tight">Models</h1>
		<p class="text-sm text-muted-foreground">
			What clients can ask for, how names are rewritten, and which account answers.
		</p>
	</div>

	<Tabs.Root value={tab} onValueChange={selectTab}>
		<Tabs.List>
			{#each tabs as t (t.id)}
				<Tabs.Trigger value={t.id}>{t.label}</Tabs.Trigger>
			{/each}
		</Tabs.List>

		<Tabs.Content value="catalog" class="space-y-6 pt-4">
			<div class="flex flex-wrap items-center justify-between gap-3">
				<p class="text-sm text-muted-foreground">
					Clients can use either name: <code>claude-opus-5</code> or
					<code>anthropic/claude-opus-5</code>. Prefix with <code>claude/</code> or
					<code>codex/</code> to pin the provider.
				</p>
				<Button variant="outline" onclick={() => (customOpen = true)}><PlusIcon /> Add model</Button
				>
			</div>
			{#each data.labs as lab (lab.id)}
				{@const list = catalog.filter((m) => m.lab === lab.id)}
				{#if list.length}
					<Card.Root>
						<Card.Header>
							<Card.Title class="flex items-center gap-2">
								<LabLogo lab={lab.id} class="size-5" />
								{lab.name}
							</Card.Title>
						</Card.Header>
						<Card.Content>
							<Table.Root>
								<Table.Header>
									<Table.Row>
										<Table.Head>Model</Table.Head>
										<Table.Head>Served by</Table.Head>
										<Table.Head class="text-right">Context</Table.Head>
										<Table.Head>Routing</Table.Head>
										<Table.Head>Enabled</Table.Head>
										<Table.Head class="w-0"></Table.Head>
									</Table.Row>
								</Table.Header>
								<Table.Body>
									{#each list as m (m.canonical)}
										{@const o = ov(m.canonical)}
										{@const serving = servingAccounts(m)}
										<Table.Row class={o.disabled ? 'opacity-60' : ''}>
											<Table.Cell>
												<p class="flex items-center gap-2 font-medium">
													{o.name || m.name}
													{#if m.custom}<Badge variant="secondary">Custom</Badge>{/if}
												</p>
												<p class="font-mono text-xs text-muted-foreground">{m.canonical}</p>
											</Table.Cell>
											<Table.Cell>
												<span class="flex items-center gap-1.5">
													<ProviderLogo provider={m.provider} />
													{providerName[m.provider]}
												</span>
												<p class="text-xs {serving ? 'text-muted-foreground' : 'text-destructive'}">
													{serving
														? `${serving} account${serving === 1 ? '' : 's'}${(o.accounts ?? []).length ? ' (picked)' : ''}`
														: 'no accounts'}
												</p>
											</Table.Cell>
											<Table.Cell class="text-right tabular-nums">
												{m.contextLength ? compact(m.contextLength) : '—'}
											</Table.Cell>
											<Table.Cell class="text-xs text-muted-foreground">
												{o.strategy
													? strategyLabel[o.strategy]
													: `${strategyLabel[rs.providerStrategy[m.provider] ?? rs.strategy]} (default)`}
												{#if o.effort}<br />effort {o.effort} by default{/if}
											</Table.Cell>
											<Table.Cell>
												<Switch
													checked={!o.disabled}
													aria-label="Enabled"
													onCheckedChange={(v) => setOv(m.canonical, { ...o, disabled: !v })}
												/>
											</Table.Cell>
											<Table.Cell class="whitespace-nowrap">
												<Button
													variant="ghost"
													size="icon-sm"
													aria-label="Model settings"
													onclick={() => {
														editing = m;
														editOpen = true;
													}}
												>
													<Settings2Icon />
												</Button>
												{#if m.custom}
													<Button
														variant="ghost"
														size="icon-sm"
														aria-label="Remove model"
														onclick={() => removeCustom(m)}
													>
														<Trash2Icon />
													</Button>
												{/if}
											</Table.Cell>
										</Table.Row>
									{/each}
								</Table.Body>
							</Table.Root>
						</Card.Content>
					</Card.Root>
				{/if}
			{/each}
		</Tabs.Content>

		<Tabs.Content value="mapping" class="space-y-6 pt-4">
			<Card.Root>
				<Card.Header>
					<Card.Title>Global rules</Card.Title>
					<Card.Description>
						Rewrite the model a client asks for, e.g. send Claude Code's background
						<code>claude-haiku-*</code> calls to <code>gpt-5.5</code> with low effort. Keys can add their
						own rules on top, or ignore these.
					</Card.Description>
				</Card.Header>
				<Card.Content>
					<RulesEditor bind:rules={rs.mappings} {catalog} idPrefix="global" />
				</Card.Content>
			</Card.Root>
			<Card.Root>
				<Card.Header>
					<Card.Title>Try it</Card.Title>
					<Card.Description>Uses the rules above, including unsaved changes.</Card.Description>
				</Card.Header>
				<Card.Content>
					<RouteTester routing={rs} listId="catalog-models" />
				</Card.Content>
			</Card.Root>
		</Tabs.Content>

		<Tabs.Content value="requests" class="space-y-6 pt-4">
			<Card.Root>
				<Card.Header>
					<Card.Title>Request rules</Card.Title>
					<Card.Description>
						Set, override or remove fields in what's sent upstream, e.g. force
						<code>service_tier</code> to <code>"priority"</code> for Codex, or drop
						<code>temperature</code> for one model.
					</Card.Description>
				</Card.Header>
				<Card.Content>
					<PayloadRulesEditor bind:rules={rs.payloadRules} {catalog} />
				</Card.Content>
			</Card.Root>
		</Tabs.Content>

		<Tabs.Content value="images" class="space-y-6 pt-4">
			<Card.Root>
				<Card.Header>
					<Card.Title>Image generation</Card.Title>
					<Card.Description>
						<code>/v1/images/generations</code> and <code>/v1/images/edits</code> are served by Codex
						accounts: a small model is asked to call ChatGPT's image tool, and the picture is returned
						in the OpenAI Images format.
					</Card.Description>
				</Card.Header>
				<Card.Content>
					<Field.Group>
						<Field.Field orientation="horizontal">
							<Field.Content>
								<Field.Label for="img-on">Serve the Images API</Field.Label>
							</Field.Content>
							<Switch id="img-on" bind:checked={rs.images.enabled} />
						</Field.Field>
						<Field.Field>
							<Field.Label for="img-model">Model that runs the image tool</Field.Label>
							<Input
								id="img-model"
								class="w-64 font-mono text-xs"
								list="catalog-models"
								bind:value={rs.images.model}
							/>
							<Field.Description>
								Image requests are routed, and checked against a key's allowed models, as this
								model.
							</Field.Description>
						</Field.Field>
						<Field.Field>
							<Field.Label for="img-tool">Default image model</Field.Label>
							<Input
								id="img-tool"
								class="w-64 font-mono text-xs"
								bind:value={rs.images.toolModel}
							/>
							<Field.Description>
								Used when a request doesn't ask for a <code>gpt-image-…</code> model.
							</Field.Description>
						</Field.Field>
						<Field.Field orientation="horizontal">
							<Field.Content>
								<Field.Label for="img-auto"
									>Let Codex models draw in Responses API requests</Field.Label
								>
								<Field.Description>
									Adds the image tool to every Responses API request sent to Codex (except free
									accounts), like the Codex app does. Chat Completions and Anthropic clients can't
									show images, so they never get it.
								</Field.Description>
							</Field.Content>
							<Switch id="img-auto" bind:checked={rs.images.autoTool} />
						</Field.Field>
					</Field.Group>
				</Card.Content>
			</Card.Root>
		</Tabs.Content>

		<Tabs.Content value="routing" class="space-y-6 pt-4">
			<Card.Root>
				<Card.Header>
					<Card.Title>Picking an account</Card.Title>
					<Card.Description>
						Account priority always comes first; this decides between accounts with the same
						priority. Individual models can override it.
					</Card.Description>
				</Card.Header>
				<Card.Content>
					<Field.Group>
						<RadioGroup.Root bind:value={rs.strategy} class="gap-3">
							{#each strategyOptions as s (s.value)}
								<div class="flex items-start gap-3">
									<RadioGroup.Item value={s.value} id="st-{s.value}" class="mt-0.5" />
									<Label for="st-{s.value}" class="flex flex-col items-start gap-0.5 font-normal">
										<span class="font-medium">{s.label}</span>
										<span class="text-muted-foreground">{s.hint}</span>
									</Label>
								</div>
							{/each}
						</RadioGroup.Root>
						<div class="grid gap-4 sm:grid-cols-2">
							{#each ['claude', 'codex'] as const as p (p)}
								<Field.Field>
									<Field.Label class="gap-1.5"
										><ProviderLogo provider={p} /> {providerName[p]}</Field.Label
									>
									<Select.Root
										type="single"
										value={rs.providerStrategy[p] ?? ''}
										onValueChange={(v) => {
											const next = { ...rs.providerStrategy };
											if (v) next[p] = v as Strategy;
											else delete next[p];
											rs.providerStrategy = next;
										}}
									>
										<Select.Trigger class="w-full">
											{rs.providerStrategy[p]
												? strategyLabel[rs.providerStrategy[p]!]
												: 'Same as above'}
										</Select.Trigger>
										<Select.Content>
											<Select.Item value="" label="Same as above">Same as above</Select.Item>
											{#each strategyOptions as s (s.value)}
												<Select.Item value={s.value} label={s.label}>{s.label}</Select.Item>
											{/each}
										</Select.Content>
									</Select.Root>
								</Field.Field>
							{/each}
						</div>
						<Field.Set>
							<Field.Legend variant="label">Keep related requests on the same account</Field.Legend>
							<RadioGroup.Root bind:value={rs.affinity} class="gap-3">
								{#each affinityOptions as o (o.value)}
									<div class="flex items-start gap-3">
										<RadioGroup.Item value={o.value} id="af-{o.value}" class="mt-0.5" />
										<Label for="af-{o.value}" class="flex flex-col items-start gap-0.5 font-normal">
											<span class="font-medium">{o.label}</span>
											<span class="text-muted-foreground">{o.hint}</span>
										</Label>
									</div>
								{/each}
							</RadioGroup.Root>
							<Field.Description>
								Conversations are recognised by the client's session id when it sends one, otherwise
								by how they start. If the account becomes unavailable, the conversation moves on.
							</Field.Description>
						</Field.Set>
						{#if rs.affinity !== 'off'}
							<Field.Field>
								<Field.Label for="sticky-min"
									>Forget the account after idle for (minutes)</Field.Label
								>
								<Input
									id="sticky-min"
									type="number"
									min={1}
									class="w-32"
									bind:value={rs.stickyMinutes}
								/>
							</Field.Field>
						{/if}
					</Field.Group>
				</Card.Content>
			</Card.Root>

			<Card.Root>
				<Card.Header>
					<Card.Title>Model names</Card.Title>
				</Card.Header>
				<Card.Content>
					<Field.Group>
						<Field.Field orientation="horizontal">
							<Field.Content>
								<Field.Label for="unknown">Allow models that aren't in the catalog</Field.Label>
								<Field.Description>
									Names like <code>claude-…</code> or <code>gpt-…</code> are sent to the matching provider
									even if they're new. Turn off to only allow the catalog.
								</Field.Description>
							</Field.Content>
							<Switch id="unknown" bind:checked={rs.unknownModels} />
						</Field.Field>
						<Field.Set>
							<Field.Legend variant="label">Names in <code>/v1/models</code></Field.Legend>
							<RadioGroup.Root bind:value={rs.modelList} class="gap-2.5">
								{#each [{ v: 'bare', l: 'Plain (claude-opus-5)' }, { v: 'canonical', l: 'With lab (anthropic/claude-opus-5)' }, { v: 'both', l: 'Both' }] as o (o.v)}
									<div class="flex items-center gap-3">
										<RadioGroup.Item value={o.v} id="ml-{o.v}" />
										<Label for="ml-{o.v}" class="font-normal">{o.l}</Label>
									</div>
								{/each}
							</RadioGroup.Root>
							<Field.Description>
								The OpenRouter-style path (<code>/api/v1/models</code>) always uses names with the
								lab.
							</Field.Description>
						</Field.Set>
					</Field.Group>
				</Card.Content>
			</Card.Root>

			<Card.Root>
				<Card.Header>
					<Card.Title>Chat Completions responses</Card.Title>
				</Card.Header>
				<Card.Content>
					<Field.Set>
						<Field.Legend variant="label">Put reasoning text in</Field.Legend>
						<RadioGroup.Root bind:value={rs.reasoningField} class="gap-2.5">
							{#each [{ v: 'both', l: 'Both fields (works with most clients)' }, { v: 'reasoning_content', l: 'reasoning_content (DeepSeek style)' }, { v: 'reasoning', l: 'reasoning (OpenRouter style)' }] as o (o.v)}
								<div class="flex items-center gap-3">
									<RadioGroup.Item value={o.v} id="rf-{o.v}" />
									<Label for="rf-{o.v}" class="font-normal">{o.l}</Label>
								</div>
							{/each}
						</RadioGroup.Root>
					</Field.Set>
				</Card.Content>
			</Card.Root>
		</Tabs.Content>
	</Tabs.Root>

	<UnsavedBar dirty={draft.dirty} {saving} onSave={save} onReset={() => draft.reset()} />
</div>

<ModelDialog
	model={editing}
	override={editing ? ov(editing.canonical) : emptyOverride}
	accounts={data.accounts}
	bind:open={editOpen}
	onApply={(o) => editing && setOv(editing.canonical, o)}
/>
<CustomModelDialog
	bind:open={customOpen}
	existing={catalog.flatMap((m) => [m.id.toLowerCase(), m.canonical.toLowerCase()])}
	onAdd={addCustom}
/>
