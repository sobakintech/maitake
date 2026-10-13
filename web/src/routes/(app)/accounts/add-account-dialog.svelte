<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import { admin, errorMessage, type Provider } from '#lib/api.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import * as RadioGroup from '#lib/components/ui/radio-group/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import ProviderLogo from '#lib/components/provider-logo.svelte';
	import FileJsonIcon from '@lucide/svelte/icons/file-json';
	import UploadIcon from '@lucide/svelte/icons/upload';
	import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
	import LoaderIcon from '@lucide/svelte/icons/loader-2';
	import { toast } from 'svelte-sonner';

	let { open = $bindable(false), provider = 'claude' }: { open: boolean; provider?: Provider } =
		$props();

	let tab = $state<'claude' | 'codex' | 'import'>('claude');
	let codexMethod = $state<'device' | 'browser'>('device');
	let busy = $state(false);
	let error = $state('');

	let oauth = $state<{
		provider: Provider;
		sessionId: string;
		authUrl: string;
		redirectUri: string;
	} | null>(null);
	let callback = $state('');

	let device = $state<{ sessionId: string; userCode: string; verificationUrl: string } | null>(
		null
	);
	let pollTimer: ReturnType<typeof setInterval> | undefined;

	let importProvider = $state<Provider>('codex');
	let importJSON = $state('');
	let importFile = $state('');
	let dragging = $state(false);
	let fileInput = $state<HTMLInputElement | null>(null);

	async function loadFile(file: File | undefined) {
		if (!file) return;
		if (file.size > 1 << 20) {
			error = 'That file is too large to be a credentials file.';
			return;
		}
		importJSON = await file.text();
		importFile = file.name;
		error = '';
		// Guess the provider from the file contents so the radio matches what was dropped.
		if (/"claudeAiOauth"|"type"\s*:\s*"claude"/.test(importJSON)) importProvider = 'claude';
		else if (/"tokens"\s*:|"type"\s*:\s*"codex"|chatgpt_account_id/.test(importJSON))
			importProvider = 'codex';
	}

	function reset() {
		stopPolling();
		if (device) admin.cancelDevice(device.sessionId).catch(() => {});
		oauth = null;
		device = null;
		callback = '';
		importJSON = '';
		importFile = '';
		error = '';
		busy = false;
	}

	$effect(() => {
		if (open) {
			tab = provider;
			importProvider = provider;
		}
	});

	$effect(() => {
		if (!open) reset();
	});

	function stopPolling() {
		if (pollTimer) clearInterval(pollTimer);
		pollTimer = undefined;
	}

	async function done() {
		toast.success('Account added');
		device = null;
		open = false;
		await invalidateAll();
	}

	async function startBrowser(provider: Provider) {
		busy = true;
		error = '';
		try {
			const res = await admin.startOAuth(provider);
			oauth = { provider, ...res };
			window.open(res.authUrl, '_blank', 'noopener');
		} catch (e) {
			error = errorMessage(e);
		} finally {
			busy = false;
		}
	}

	async function complete() {
		if (!oauth) return;
		busy = true;
		error = '';
		try {
			await admin.completeOAuth(oauth.sessionId, callback);
			await done();
		} catch (e) {
			error = errorMessage(e);
		} finally {
			busy = false;
		}
	}

	async function startDevice() {
		busy = true;
		error = '';
		try {
			device = await admin.startDevice();
			pollTimer = setInterval(async () => {
				if (!device) return stopPolling();
				try {
					const s = await admin.deviceStatus(device.sessionId);
					if (s.status === 'done' && s.account) {
						stopPolling();
						await done();
					} else if (s.status === 'error') {
						stopPolling();
						error = s.error ?? 'Login failed';
						device = null;
					}
				} catch (e) {
					stopPolling();
					error = errorMessage(e);
				}
			}, 3000);
		} catch (e) {
			error = errorMessage(e);
		} finally {
			busy = false;
		}
	}

	async function doImport() {
		busy = true;
		error = '';
		try {
			await admin.importAccount(importProvider, importJSON);
			await done();
		} catch (e) {
			error = errorMessage(e);
		} finally {
			busy = false;
		}
	}
</script>

{#snippet pasteFlow()}
	{#if oauth}
		<div class="space-y-4">
			<ol class="list-decimal space-y-1 pl-5 text-sm text-muted-foreground">
				<li>
					Sign in on the page that just opened (<a
						href={oauth.authUrl}
						target="_blank"
						rel="noreferrer"
						class="inline-flex items-center gap-1 underline underline-offset-4"
						>open it again <ExternalLinkIcon class="size-3" /></a
					>).
				</li>
				<li>
					You'll land on a <code class="text-xs"
						>{oauth.redirectUri.split('/').slice(0, 3).join('/')}</code
					> page that doesn't load. That's expected.
				</li>
				<li>Copy the full URL from the address bar and paste it below.</li>
			</ol>
			<Field.Field data-invalid={!!error}>
				<Field.Label for="callback">Callback URL</Field.Label>
				<Textarea
					id="callback"
					rows={3}
					class="font-mono text-xs"
					placeholder={`${oauth.redirectUri}?code=…&state=…`}
					bind:value={callback}
				/>
				{#if error}<Field.Error>{error}</Field.Error>{/if}
			</Field.Field>
			<div class="flex justify-end gap-2">
				<Button variant="outline" onclick={() => (oauth = null)}>Back</Button>
				<Button onclick={complete} disabled={busy || !callback.trim()}>
					{#if busy}<LoaderIcon class="animate-spin" />{/if} Add account
				</Button>
			</div>
		</div>
	{/if}
{/snippet}

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-lg">
		<Dialog.Header>
			<Dialog.Title>Add account</Dialog.Title>
			<Dialog.Description
				>Log in to a Claude or ChatGPT subscription, or import existing credentials.</Dialog.Description
			>
		</Dialog.Header>
		<Tabs.Root bind:value={tab} onValueChange={reset}>
			<Tabs.List class="w-full">
				<Tabs.Trigger value="claude" class="gap-2"
					><ProviderLogo provider="claude" /> Claude</Tabs.Trigger
				>
				<Tabs.Trigger value="codex" class="gap-2"
					><ProviderLogo provider="codex" /> Codex</Tabs.Trigger
				>
				<Tabs.Trigger value="import" class="gap-2"><FileJsonIcon /> Import</Tabs.Trigger>
			</Tabs.List>

			<Tabs.Content value="claude" class="pt-4">
				{#if oauth}
					{@render pasteFlow()}
				{:else}
					<div class="space-y-4">
						<p class="text-sm text-muted-foreground">
							Opens Claude's login page. Use the account whose Pro or Max subscription you want to
							share.
						</p>
						{#if error}<p class="text-sm text-destructive">{error}</p>{/if}
						<Button onclick={() => startBrowser('claude')} disabled={busy}>
							{#if busy}<LoaderIcon class="animate-spin" />{:else}<ProviderLogo
									provider="claude"
								/>{/if} Log in with Claude
						</Button>
					</div>
				{/if}
			</Tabs.Content>

			<Tabs.Content value="codex" class="pt-4">
				{#if oauth}
					{@render pasteFlow()}
				{:else if device}
					<div class="space-y-4 text-sm">
						<p>
							Open
							<a
								href={device.verificationUrl}
								target="_blank"
								rel="noreferrer"
								class="inline-flex items-center gap-1 font-medium underline underline-offset-4"
								>{device.verificationUrl} <ExternalLinkIcon class="size-3" /></a
							>
							and enter this code:
						</p>
						<p
							class="rounded-lg border bg-muted/40 py-3 text-center font-mono text-2xl font-semibold tracking-widest"
						>
							{device.userCode}
						</p>
						<p class="flex items-center gap-2 text-muted-foreground">
							<LoaderIcon class="size-4 animate-spin" /> Waiting for you to approve…
						</p>
						<div class="flex justify-end">
							<Button variant="outline" onclick={reset}>Cancel</Button>
						</div>
					</div>
				{:else}
					<div class="space-y-4">
						<RadioGroup.Root bind:value={codexMethod} class="gap-3">
							<div class="flex items-start gap-3">
								<RadioGroup.Item value="device" id="m-device" class="mt-0.5" />
								<Label for="m-device" class="flex flex-col items-start gap-0.5 font-normal">
									<span class="font-medium">Device code</span>
									<span class="text-muted-foreground"
										>Enter a short code on OpenAI's site. Works from anywhere.</span
									>
								</Label>
							</div>
							<div class="flex items-start gap-3">
								<RadioGroup.Item value="browser" id="m-browser" class="mt-0.5" />
								<Label for="m-browser" class="flex flex-col items-start gap-0.5 font-normal">
									<span class="font-medium">Browser login</span>
									<span class="text-muted-foreground"
										>Sign in, then paste the callback URL back here.</span
									>
								</Label>
							</div>
						</RadioGroup.Root>
						{#if error}<p class="text-sm text-destructive">{error}</p>{/if}
						<Button
							onclick={() => (codexMethod === 'device' ? startDevice() : startBrowser('codex'))}
							disabled={busy}
						>
							{#if busy}<LoaderIcon class="animate-spin" />{:else}<ProviderLogo
									provider="codex"
								/>{/if} Log in with ChatGPT
						</Button>
					</div>
				{/if}
			</Tabs.Content>

			<Tabs.Content value="import" class="pt-4">
				<div class="space-y-4">
					<RadioGroup.Root bind:value={importProvider} class="flex gap-6">
						<div class="flex items-center gap-2">
							<RadioGroup.Item value="codex" id="i-codex" />
							<Label for="i-codex" class="gap-1.5 font-normal"
								><ProviderLogo provider="codex" /> Codex</Label
							>
						</div>
						<div class="flex items-center gap-2">
							<RadioGroup.Item value="claude" id="i-claude" />
							<Label for="i-claude" class="gap-1.5 font-normal"
								><ProviderLogo provider="claude" /> Claude</Label
							>
						</div>
					</RadioGroup.Root>
					<Field.Field data-invalid={!!error}>
						<div class="flex items-center justify-between gap-2">
							<Field.Label for="import-json">Credentials JSON</Field.Label>
							<input
								bind:this={fileInput}
								type="file"
								accept=".json,application/json"
								class="hidden"
								onchange={(e) => loadFile(e.currentTarget.files?.[0])}
							/>
							<Button type="button" variant="outline" size="sm" onclick={() => fileInput?.click()}>
								<UploadIcon /> Choose file
							</Button>
						</div>
						<div
							role="group"
							aria-label="Drop a credentials file"
							class="relative rounded-lg"
							ondragover={(e) => {
								e.preventDefault();
								dragging = true;
							}}
							ondragleave={() => (dragging = false)}
							ondrop={(e) => {
								e.preventDefault();
								dragging = false;
								loadFile(e.dataTransfer?.files[0]);
							}}
						>
							<Textarea
								id="import-json"
								rows={6}
								class="font-mono text-xs"
								placeholder="Paste the file's contents, or drop the file here"
								bind:value={importJSON}
								oninput={() => (importFile = '')}
							/>
							{#if dragging}
								<div
									class="pointer-events-none absolute inset-0 flex items-center justify-center rounded-lg border-2 border-dashed border-primary bg-background/80 text-sm font-medium"
								>
									Drop to load
								</div>
							{/if}
						</div>
						{#if importFile}
							<p class="text-xs text-muted-foreground">Loaded {importFile}</p>
						{/if}
						<Field.Description>
							{#if importProvider === 'codex'}
								Upload or paste <code>~/.codex/auth.json</code> or a CLIProxyAPI
								<code>codex-*.json</code> file.
							{:else}
								Upload or paste <code>~/.claude/.credentials.json</code> or a CLIProxyAPI
								<code>claude-*.json</code> file.
							{/if}
							The tokens are refreshed right away to check they work.
						</Field.Description>
						{#if error}<Field.Error>{error}</Field.Error>{/if}
					</Field.Field>
					<div class="flex justify-end">
						<Button onclick={doImport} disabled={busy || !importJSON.trim()}>
							{#if busy}<LoaderIcon class="animate-spin" />{/if} Import
						</Button>
					</div>
				</div>
			</Tabs.Content>
		</Tabs.Root>
	</Dialog.Content>
</Dialog.Root>
