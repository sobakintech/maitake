<script lang="ts">
	let { label, value, detail }: { label: string; value: string; detail?: string } = $props();
</script>

<div class="rounded-xl border bg-card p-4">
	<p class="text-sm text-muted-foreground">{label}</p>
	<p class="mt-1 text-2xl font-semibold tracking-tight">{value}</p>
	{#if detail}<p class="mt-1 text-xs text-muted-foreground">{detail}</p>{/if}
</div>
