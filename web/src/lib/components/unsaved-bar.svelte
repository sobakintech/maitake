<script lang="ts">
	import { beforeNavigate } from '$app/navigation';
	import { Button } from '#lib/components/ui/button/index.js';
	import { useSidebar } from '#lib/components/ui/sidebar/index.js';
	import { fly } from 'svelte/transition';

	// Discord-style save bar: appears while there are unsaved changes, blocks leaving the page,
	// and shakes when someone tries to.
	let {
		dirty,
		saving = false,
		onSave,
		onReset
	}: { dirty: boolean; saving?: boolean; onSave: () => void; onReset: () => void } = $props();

	const sidebar = useSidebar();
	let nudged = $state(false);
	let nudgeTimer: ReturnType<typeof setTimeout> | undefined;

	function nudge() {
		nudged = false;
		clearTimeout(nudgeTimer);
		// Restart the animation on the next frame so repeated attempts shake again.
		requestAnimationFrame(() => {
			nudged = true;
			nudgeTimer = setTimeout(() => (nudged = false), 1200);
		});
	}

	beforeNavigate((nav) => {
		if (!dirty || saving) return;
		// For a tab close or reload, cancelling shows the browser's own "leave site?" prompt.
		nav.cancel();
		if (nav.type !== 'leave') nudge();
	});

	function onkeydown(e: KeyboardEvent) {
		if (dirty && !saving && (e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 's') {
			e.preventDefault();
			onSave();
		}
	}
</script>

<svelte:window {onkeydown} />

{#if dirty}
	<!-- Keeps the end of the page scrollable past the bar. -->
	<div class="h-16" aria-hidden="true"></div>
	<div
		class={[
			'pointer-events-none fixed inset-x-0 bottom-4 z-40 flex justify-center px-6 transition-[left] duration-200 ease-linear',
			sidebar &&
				!sidebar.isMobile &&
				(sidebar.state === 'collapsed'
					? 'md:left-(--sidebar-width-icon)'
					: 'md:left-(--sidebar-width)')
		]}
	>
		<div
			transition:fly={{ y: 24, duration: 180 }}
			class={[
				'pointer-events-auto flex w-full max-w-2xl items-center justify-between gap-4 rounded-xl border px-4 py-2.5 shadow-lg transition-colors',
				nudged ? 'unsaved-shake border-destructive bg-destructive text-white' : 'bg-popover'
			]}
			role="status"
		>
			<p class="text-sm font-medium">
				{nudged ? 'Careful — you have unsaved changes!' : 'You have unsaved changes'}
			</p>
			<div class="flex shrink-0 items-center gap-2">
				<Button
					variant="ghost"
					size="sm"
					class={nudged ? 'text-white hover:bg-white/15 hover:text-white' : ''}
					onclick={onReset}
					disabled={saving}
				>
					Reset
				</Button>
				<Button size="sm" onclick={onSave} disabled={saving}>Save changes</Button>
			</div>
		</div>
	</div>
{/if}

<style>
	.unsaved-shake {
		animation: unsaved-shake 0.45s cubic-bezier(0.36, 0.07, 0.19, 0.97);
	}

	@keyframes unsaved-shake {
		10%,
		90% {
			transform: translateX(-2px);
		}
		20%,
		80% {
			transform: translateX(4px);
		}
		30%,
		50%,
		70% {
			transform: translateX(-8px);
		}
		40%,
		60% {
			transform: translateX(8px);
		}
	}
</style>
