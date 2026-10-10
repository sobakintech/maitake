<script lang="ts">
	import { goto, invalidateAll } from '$app/navigation';
	import { page } from '$app/state';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import * as AlertDialog from '#lib/components/ui/alert-dialog/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import { dateTime, relativeTime } from '#lib/format.js';
	import { admin, errorMessage, type Settings } from '#lib/api.js';
	import { Draft } from '#lib/draft.svelte.js';
	import UnsavedBar from '#lib/components/unsaved-bar.svelte';
	import ErrorRulesEditor from '#lib/components/error-rules-editor.svelte';
	import RedeemPolicyFields from '#lib/components/redeem-policy-fields.svelte';
	import { numbers } from '#lib/redeem.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import LineListInput from '#lib/components/line-list-input.svelte';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	const draft = new Draft<Settings>(untrack(() => data.settings));
	$effect.pre(() => draft.sync(data.settings));
	const form = $derived(draft.value);
	let saving = $state(false);

	const tabs = [
		{ id: 'pool', label: 'Account pool' },
		{ id: 'claude', label: 'Claude' },
		{ id: 'usage', label: 'Usage history' },
		{ id: 'audit', label: 'Audit log' },
		{ id: 'keys', label: 'API keys' },
		{ id: 'about', label: 'About' }
	];
	const tab = $derived(
		tabs.some((t) => t.id === page.url.searchParams.get('tab'))
			? page.url.searchParams.get('tab')!
			: 'pool'
	);

	function selectTab(v: string) {
		const url = new URL(page.url.href);
		url.searchParams.set('tab', v);
		goto(url, { replace: true, reset: false });
	}

	let updates = $derived(data.updates);
	let checking = $state(false);

	async function checkNow() {
		checking = true;
		try {
			updates = (await admin.checkUpdates()).updates;
			if (updates.error) toast.error(updates.error);
			else await invalidateAll();
		} catch (err) {
			toast.error(errorMessage(err));
		} finally {
			checking = false;
		}
	}

	// Sensitive words are edited one per line.
	let wordsText = $state(untrack(() => data.settings.claude.sensitiveWords.join('\n')));
	$effect.pre(() => {
		const words = form.claude.sensitiveWords.join('\n');
		untrack(() => {
			if (normWords(wordsText).join('\n') !== words) wordsText = words;
		});
	});
	function normWords(text: string) {
		return text
			.split('\n')
			.map((w) => w.trim())
			.filter(Boolean);
	}
	let clearOpen = $state(false);
	let clearAuditOpen = $state(false);
	const modelPatterns = 'claude-*\nopenai/gpt-5*';

	const recordLabels = {
		all: 'Every request',
		errors: 'Only failed requests',
		success: 'Only successful requests'
	} as const;

	async function clearAudit() {
		clearAuditOpen = false;
		try {
			const { deleted } = await admin.clearAudit({});
			toast.success(`Deleted ${deleted} ${deleted === 1 ? 'entry' : 'entries'}`);
		} catch (err) {
			toast.error(errorMessage(err));
		}
	}

	async function clearCopies() {
		clearOpen = false;
		try {
			const { cleared } = await admin.clearKeyCopies();
			toast.success(`Deleted ${cleared} stored ${cleared === 1 ? 'copy' : 'copies'}`);
		} catch (err) {
			toast.error(errorMessage(err));
		}
	}
	async function save() {
		saving = true;
		try {
			const next = draft.snapshot();
			next.pool.quotaPollMinutes = Number(next.pool.quotaPollMinutes);
			next.pool.maxRetries = Number(next.pool.maxRetries);
			numbers(next.pool.redeem);
			next.pool.bootstrapSeconds = Number(next.pool.bootstrapSeconds);
			next.pool.keepaliveSeconds = Number(next.pool.keepaliveSeconds);
			for (const r of next.pool.errorRules) {
				r.status = Number(r.status) || 0;
				r.cooldownSeconds = Number(r.cooldownSeconds) || 0;
			}
			next.usageRetentionDays = Number(next.usageRetentionDays);
			next.updates.intervalHours = Number(next.updates.intervalHours) || 6;
			for (const k of [
				'samplePercent',
				'maxBodyKB',
				'retentionDays',
				'maxEntries',
				'maxStorageMB'
			] as const)
				next.audit[k] = Number(next.audit[k]) || 0;
			const { settings } = await admin.saveSettings(next);
			draft.value = structuredClone(settings);
			draft.commit();
			toast.success('Saved');
			await invalidateAll();
		} catch (err) {
			toast.error(errorMessage(err));
		} finally {
			saving = false;
		}
	}
</script>

<div class="mx-auto max-w-3xl space-y-6">
	<div>
		<h1 class="text-xl font-semibold tracking-tight">Settings</h1>
		<p class="text-sm text-muted-foreground">
			How the proxy behaves, and how clients connect to it.
		</p>
	</div>

	<form
		class="space-y-6"
		onsubmit={(e) => {
			e.preventDefault();
			save();
		}}
	>
		<Tabs.Root value={tab} onValueChange={selectTab}>
			<Tabs.List>
				{#each tabs as t (t.id)}
					<Tabs.Trigger value={t.id}>{t.label}</Tabs.Trigger>
				{/each}
			</Tabs.List>

			<Tabs.Content value="pool" class="pt-4">
				<Card.Root>
					<Card.Header>
						<Card.Title>Account pool</Card.Title>
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
									When an account is rate limited or errors before answering, the request moves to
									the next one.
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
				<Card.Root class="mt-6">
					<Card.Header>
						<Card.Title>Banked resets</Card.Title>
						<Card.Description>
							When Codex accounts spend their banked resets on their own. Each account can override
							any of this in its Edit dialog.
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
				<Card.Root class="mt-6">
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
			</Tabs.Content>

			<Tabs.Content value="claude" class="pt-4">
				<Card.Root>
					<Card.Header>
						<Card.Title>Look like Claude Code</Card.Title>
						<Card.Description>
							Requests to Claude subscription accounts always carry Claude Code's headers, system
							prompt and request signing. These add finer details.
						</Card.Description>
					</Card.Header>
					<Card.Content>
						<Field.Group>
							<Field.Field orientation="horizontal">
								<Field.Content>
									<Field.Label for="tool-aliases">Rename tools like MCP tools</Field.Label>
									<Field.Description>
										Your client's tools are sent as <code>mcp__…</code> names, the way Claude Code sends
										tools from MCP servers, and renamed back in the response.
									</Field.Description>
								</Field.Content>
								<Switch id="tool-aliases" bind:checked={form.claude.toolAliases} />
							</Field.Field>
							<Field.Field orientation="horizontal">
								<Field.Content>
									<Field.Label for="cache-bp">Place prompt cache breakpoints</Field.Label>
									<Field.Description>
										Marks the end of the conversation for caching the way Claude Code does, when the
										client didn't mark anything itself. Cached input is cheaper on your limits.
									</Field.Description>
								</Field.Content>
								<Switch id="cache-bp" bind:checked={form.claude.cacheBreakpoints} />
							</Field.Field>
							<Field.Field orientation="horizontal">
								<Field.Content>
									<Field.Label for="cache-ttl">Keep caches for an hour</Field.Label>
									<Field.Description>
										Uses the 1-hour cache lifetime instead of 5 minutes, like Claude Code on
										subscriptions.
									</Field.Description>
								</Field.Content>
								<Switch id="cache-ttl" bind:checked={form.claude.longCacheTTL} />
							</Field.Field>
							<Field.Field orientation="horizontal">
								<Field.Content>
									<Field.Label for="tls">Match Claude Code's TLS fingerprint</Field.Label>
									<Field.Description>
										{#if data.outboundProxy}
											Unavailable while <code>proxy-url</code> is set in <code>config.yaml</code>:
											the proxy makes its own connection to Anthropic.
										{:else}
											Connections to Anthropic open with the same TLS handshake as Claude Code
											(which runs on Node) instead of Go's.
										{/if}
									</Field.Description>
								</Field.Content>
								<Switch
									id="tls"
									disabled={data.outboundProxy}
									bind:checked={form.claude.tlsFingerprint}
								/>
							</Field.Field>
							<Field.Field>
								<Field.Label for="words">Words to disguise</Field.Label>
								<Textarea
									id="words"
									rows={4}
									class="font-mono text-xs"
									placeholder="one word per line"
									bind:value={wordsText}
									oninput={() => (form.claude.sensitiveWords = normWords(wordsText))}
								/>
								<Field.Description>
									Each is sent with an invisible zero-width space after its first letter, so it no
									longer matches exactly. The model still reads it normally.
								</Field.Description>
							</Field.Field>
						</Field.Group>
					</Card.Content>
				</Card.Root>
			</Tabs.Content>

			<Tabs.Content value="usage" class="pt-4">
				<Card.Root>
					<Card.Header>
						<Card.Title>Usage history</Card.Title>
					</Card.Header>
					<Card.Content>
						<Field.Field>
							<Field.Label for="retention">Keep request history for (days)</Field.Label>
							<Input
								id="retention"
								type="number"
								min={0}
								class="w-32"
								bind:value={form.usageRetentionDays}
							/>
							<Field.Description>0 keeps it forever.</Field.Description>
						</Field.Field>
					</Card.Content>
				</Card.Root>
			</Tabs.Content>

			<Tabs.Content value="audit" class="pt-4">
				<Card.Root>
					<Card.Header>
						<Card.Title>Audit log</Card.Title>
						<Card.Description>
							Keeps each request in full, headers and bodies included, for review on the
							<a href="/audit" class="underline underline-offset-4">Audit log</a> page. Bodies hold whatever
							clients send, so only turn this on when you need it.
						</Card.Description>
					</Card.Header>
					<Card.Content>
						<Field.Group>
							<Field.Field orientation="horizontal">
								<Field.Content>
									<Field.Label for="audit-on">Record requests</Field.Label>
									<Field.Description>
										Applies to every key that doesn't set its own. A key can be set to always or
										never be recorded on its page, which works even while this is off.
									</Field.Description>
								</Field.Content>
								<Switch id="audit-on" bind:checked={form.audit.enabled} />
							</Field.Field>
							<Field.Field>
								<Field.Label>Which requests</Field.Label>
								<Select.Root type="single" bind:value={form.audit.record}>
									<Select.Trigger class="w-64">{recordLabels[form.audit.record]}</Select.Trigger>
									<Select.Content>
										{#each Object.entries(recordLabels) as [v, label] (v)}
											<Select.Item value={v} {label}>{label}</Select.Item>
										{/each}
									</Select.Content>
								</Select.Root>
								<Field.Description>
									Failed means anything that ended with a status of 400 or more, including clients
									that hung up early.
								</Field.Description>
							</Field.Field>
							<Field.Field>
								<Field.Label for="audit-sample">Share of requests to record (%)</Field.Label>
								<Input
									id="audit-sample"
									type="number"
									min={1}
									max={100}
									class="w-32"
									bind:value={form.audit.samplePercent}
								/>
								<Field.Description>Picked at random; 100 records all of them.</Field.Description>
							</Field.Field>
							<Field.Field>
								<Field.Label for="audit-models">Only these models</Field.Label>
								<LineListInput
									id="audit-models"
									bind:value={form.audit.models}
									placeholder={modelPatterns}
								/>
								<Field.Description>
									Patterns like a key's allowed models, matched against the model asked for and the
									one that served it. Empty records every model.
								</Field.Description>
							</Field.Field>
						</Field.Group>
					</Card.Content>
				</Card.Root>
				<Card.Root class="mt-6">
					<Card.Header>
						<Card.Title>What to keep</Card.Title>
						<Card.Description>
							Times, tokens, the key, the account and every attempt are always kept.
						</Card.Description>
					</Card.Header>
					<Card.Content>
						<Field.Group>
							<Field.Field orientation="horizontal">
								<Field.Content>
									<Field.Label for="audit-req">Request from the client</Field.Label>
									<Field.Description>The body exactly as the client sent it.</Field.Description>
								</Field.Content>
								<Switch id="audit-req" bind:checked={form.audit.request} />
							</Field.Field>
							<Field.Field orientation="horizontal">
								<Field.Content>
									<Field.Label for="audit-upreq">Request to the provider</Field.Label>
									<Field.Description>
										After translation, mapping, payload rules and the Claude Code disguise: what the
										provider actually got. From the last account tried.
									</Field.Description>
								</Field.Content>
								<Switch id="audit-upreq" bind:checked={form.audit.upstreamRequest} />
							</Field.Field>
							<Field.Field orientation="horizontal">
								<Field.Content>
									<Field.Label for="audit-upresp">Response from the provider</Field.Label>
									<Field.Description>
										The raw stream or error before maitake converts it. From the last account tried.
									</Field.Description>
								</Field.Content>
								<Switch id="audit-upresp" bind:checked={form.audit.upstreamResponse} />
							</Field.Field>
							<Field.Field orientation="horizontal">
								<Field.Content>
									<Field.Label for="audit-resp">Response to the client</Field.Label>
									<Field.Description
										>What the client received, streamed events included.</Field.Description
									>
								</Field.Content>
								<Switch id="audit-resp" bind:checked={form.audit.response} />
							</Field.Field>
							<Field.Field orientation="horizontal">
								<Field.Content>
									<Field.Label for="audit-headers">Headers</Field.Label>
									<Field.Description>
										The client's request headers, the headers sent back, and the provider's response
										headers (rate limit details live there).
									</Field.Description>
								</Field.Content>
								<Switch id="audit-headers" bind:checked={form.audit.headers} />
							</Field.Field>
							<Field.Field>
								<Field.Label for="audit-redact">Also hide these headers</Field.Label>
								<LineListInput
									id="audit-redact"
									bind:value={form.audit.redactHeaders}
									placeholder="X-Forwarded-For"
								/>
								<Field.Description>
									Authorization, API key and cookie headers are always hidden, as is a key passed in
									the URL.
								</Field.Description>
							</Field.Field>
							<Field.Field>
								<Field.Label for="audit-max-body"
									>Keep at most this much of each body (KB)</Field.Label
								>
								<Input
									id="audit-max-body"
									type="number"
									min={0}
									class="w-32"
									bind:value={form.audit.maxBodyKB}
								/>
								<Field.Description>
									Longer bodies are cut off and marked as such. 0 keeps them whole. Conversations
									resend their whole history every turn, so this adds up.
								</Field.Description>
							</Field.Field>
						</Field.Group>
					</Card.Content>
				</Card.Root>
				<Card.Root class="mt-6">
					<Card.Header>
						<Card.Title>Retention</Card.Title>
						<Card.Description>
							Checked hourly; the oldest entries go first. Entries are stored compressed.
						</Card.Description>
					</Card.Header>
					<Card.Content>
						<Field.Group>
							<Field.Field>
								<Field.Label for="audit-days">Keep entries for (days)</Field.Label>
								<Input
									id="audit-days"
									type="number"
									min={0}
									class="w-32"
									bind:value={form.audit.retentionDays}
								/>
								<Field.Description>0 keeps them until another limit applies.</Field.Description>
							</Field.Field>
							<Field.Field>
								<Field.Label for="audit-max-entries">Keep at most (entries)</Field.Label>
								<Input
									id="audit-max-entries"
									type="number"
									min={0}
									class="w-32"
									bind:value={form.audit.maxEntries}
								/>
								<Field.Description>0 means no limit.</Field.Description>
							</Field.Field>
							<Field.Field>
								<Field.Label for="audit-max-mb">Use at most (MB of storage)</Field.Label>
								<Input
									id="audit-max-mb"
									type="number"
									min={0}
									class="w-32"
									bind:value={form.audit.maxStorageMB}
								/>
								<Field.Description>Compressed size. 0 means no limit.</Field.Description>
							</Field.Field>
							<Field.Field orientation="horizontal">
								<Field.Content>
									<Field.Label>Delete all entries</Field.Label>
									<Field.Description>Empties the audit log now.</Field.Description>
								</Field.Content>
								<Button variant="destructive" type="button" onclick={() => (clearAuditOpen = true)}>
									Delete entries
								</Button>
							</Field.Field>
						</Field.Group>
					</Card.Content>
				</Card.Root>
			</Tabs.Content>

			<Tabs.Content value="keys" class="pt-4">
				<Card.Root>
					<Card.Header>
						<Card.Title>API keys</Card.Title>
					</Card.Header>
					<Card.Content>
						<Field.Group>
							<Field.Field orientation="horizontal">
								<Field.Content>
									<Field.Label for="store-copies">Let admins copy keys after creation</Field.Label>
									<Field.Description>
										New keys are also kept encrypted, using <code>keyring.key</code> in the server's data
										folder. When off, new keys are stored only as a hash and are shown once.
									</Field.Description>
								</Field.Content>
								<Switch id="store-copies" bind:checked={form.keys.storeCopies} />
							</Field.Field>
							<Field.Field orientation="horizontal">
								<Field.Content>
									<Field.Label>Delete stored copies</Field.Label>
									<Field.Description>
										Removes the copy of every existing key. The keys keep working; they just can't
										be copied any more.
									</Field.Description>
								</Field.Content>
								<Button variant="destructive" type="button" onclick={() => (clearOpen = true)}>
									Delete copies
								</Button>
							</Field.Field>
						</Field.Group>
					</Card.Content>
				</Card.Root>
			</Tabs.Content>

			<Tabs.Content value="about" class="pt-4">
				<Card.Root>
					<Card.Header>
						<Card.Title>About</Card.Title>
					</Card.Header>
					<Card.Content class="space-y-4 text-sm">
						<dl class="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-2">
							<dt class="text-muted-foreground">Version</dt>
							<dd class="font-mono">{updates.current}</dd>
							<dt class="text-muted-foreground">Latest release</dt>
							<dd>
								{#if updates.latest}
									<a class="font-mono underline-offset-4 hover:underline" href={updates.latest.url}
										>{updates.latest.version}</a
									>
								{:else}
									<span class="text-muted-foreground">None found</span>
								{/if}
							</dd>
							<dt class="text-muted-foreground">Last checked</dt>
							<dd>
								{relativeTime(updates.checkedAt)}
								{#if updates.error}<span class="text-destructive"> · {updates.error}</span>{/if}
							</dd>
						</dl>
						{#if !/^\d+\.\d+\.\d+$/.test(updates.current)}
							<p class="text-muted-foreground">
								This is a development build, so it isn't compared against releases.
							</p>
						{/if}
						<Button variant="outline" type="button" disabled={checking} onclick={checkNow}>
							<RefreshCwIcon class={checking ? 'animate-spin' : ''} /> Check now
						</Button>
						<p class="text-muted-foreground">
							The listen address, trusted proxies, login attempt limit and outbound proxy are set in
							<code>config.yaml</code> on the server and need a restart to change.
						</p>
					</Card.Content>
				</Card.Root>

				{#if updates.newer.length}
					<Card.Root class="mt-6">
						<Card.Header>
							<Card.Title>Update available</Card.Title>
							<Card.Description>
								{updates.newer.length === 1 ? '1 release' : `${updates.newer.length} releases`} since
								{updates.current}. With Docker Compose, update with
								<code>docker compose pull && docker compose up -d</code>.
							</Card.Description>
						</Card.Header>
						<Card.Content class="space-y-6 text-sm">
							{#each updates.newer as r (r.version)}
								<div class="space-y-2">
									<div class="flex items-baseline gap-2">
										<a class="font-mono font-medium underline-offset-4 hover:underline" href={r.url}
											>{r.version}</a
										>
										<span class="text-xs text-muted-foreground">{dateTime(r.publishedAt)}</span>
									</div>
									{#each r.sections as s (s.title)}
										<div>
											<p class="text-xs font-medium text-muted-foreground">{s.title}</p>
											<ul class="list-disc space-y-0.5 pl-5">
												{#each s.items as item, i (i)}<li>{item}</li>{/each}
											</ul>
										</div>
									{:else}
										<p class="text-muted-foreground">No notes for this release.</p>
									{/each}
								</div>
							{/each}
						</Card.Content>
					</Card.Root>
				{/if}

				<Card.Root class="mt-6">
					<Card.Header>
						<Card.Title>Updates</Card.Title>
					</Card.Header>
					<Card.Content>
						<Field.Group>
							<Field.Field orientation="horizontal">
								<Field.Content>
									<Field.Label for="updates-on">Check for new releases</Field.Label>
									<Field.Description>
										Asks GitHub for maitake's releases and shows new ones here and in the sidebar.
										Nothing about this server is sent.
									</Field.Description>
								</Field.Content>
								<Switch id="updates-on" bind:checked={form.updates.enabled} />
							</Field.Field>
							{#if form.updates.enabled}
								<Field.Field>
									<Field.Label for="updates-interval">Check every (hours)</Field.Label>
									<Input
										id="updates-interval"
										type="number"
										min={1}
										max={168}
										class="w-32"
										bind:value={form.updates.intervalHours}
									/>
								</Field.Field>
							{/if}
						</Field.Group>
					</Card.Content>
				</Card.Root>
			</Tabs.Content>
		</Tabs.Root>
	</form>

	<UnsavedBar dirty={draft.dirty} {saving} onSave={save} onReset={() => draft.reset()} />
</div>

<AlertDialog.Root bind:open={clearAuditOpen}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Delete every audit log entry?</AlertDialog.Title>
			<AlertDialog.Description>
				All recorded requests are removed. Usage history isn't affected.
			</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" onclick={clearAudit}
				>Delete entries</AlertDialog.Action
			>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>

<AlertDialog.Root bind:open={clearOpen}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Delete all stored key copies?</AlertDialog.Title>
			<AlertDialog.Description>
				Existing keys keep working but can no longer be copied from the dashboard. This can't be
				undone; a key would need regenerating to be copyable again.
			</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" onclick={clearCopies}
				>Delete copies</AlertDialog.Action
			>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
