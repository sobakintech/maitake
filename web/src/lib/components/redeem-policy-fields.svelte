<script lang="ts">
	import type { RedeemPolicy } from '#lib/types.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as Select from '#lib/components/ui/select/index.js';

	// Edits everything about a banked reset policy except whether it's on.
	let { policy = $bindable(), idPrefix = 'redeem' }: { policy: RedeemPolicy; idPrefix?: string } =
		$props();

	const triggers = [
		{ value: 'any', label: 'The 5-hour or weekly limit' },
		{ value: 'weekly', label: 'Only the weekly limit' },
		{ value: 'session', label: 'Only the 5-hour limit' }
	];
	const orders = [
		{ value: 'soonest_expiry', label: 'The one expiring soonest' },
		{ value: 'latest_expiry', label: 'The one expiring last' }
	];
	const label = (list: { value: string; label: string }[], v: string) =>
		list.find((x) => x.value === v)?.label ?? v;
</script>

<Field.Group>
	<Field.Field>
		<Field.Label>Use one when this runs out</Field.Label>
		<Select.Root type="single" bind:value={policy.trigger}>
			<Select.Trigger class="w-64">{label(triggers, policy.trigger)}</Select.Trigger>
			<Select.Content>
				{#each triggers as t (t.value)}
					<Select.Item value={t.value} label={t.label}>{t.label}</Select.Item>
				{/each}
			</Select.Content>
		</Select.Root>
	</Field.Field>
	<Field.Field>
		<Field.Label for="{idPrefix}-wait">…and it resets in more than (minutes)</Field.Label>
		<Input
			id="{idPrefix}-wait"
			type="number"
			min={0}
			class="w-32"
			bind:value={policy.minWaitMinutes}
		/>
		<Field.Description
			>So a reset isn't spent minutes before the limit resets anyway.</Field.Description
		>
	</Field.Field>
	<Field.Field>
		<Field.Label>Spend</Field.Label>
		<Select.Root type="single" bind:value={policy.order}>
			<Select.Trigger class="w-64">{label(orders, policy.order)}</Select.Trigger>
			<Select.Content>
				{#each orders as o (o.value)}
					<Select.Item value={o.value} label={o.label}>{o.label}</Select.Item>
				{/each}
			</Select.Content>
		</Select.Root>
	</Field.Field>
	<div class="grid gap-4 sm:grid-cols-3">
		<Field.Field>
			<Field.Label for="{idPrefix}-reserve">Always keep</Field.Label>
			<Input id="{idPrefix}-reserve" type="number" min={0} bind:value={policy.reserve} />
			<Field.Description>banked resets unspent</Field.Description>
		</Field.Field>
		<Field.Field>
			<Field.Label for="{idPrefix}-gap">At least</Field.Label>
			<Input id="{idPrefix}-gap" type="number" min={1} bind:value={policy.gapMinutes} />
			<Field.Description>minutes between uses</Field.Description>
		</Field.Field>
		<Field.Field>
			<Field.Label for="{idPrefix}-max">At most</Field.Label>
			<Input id="{idPrefix}-max" type="number" min={0} bind:value={policy.maxPerDay} />
			<Field.Description>uses per day (0 = no limit)</Field.Description>
		</Field.Field>
	</div>
	<Field.Field>
		<Field.Label for="{idPrefix}-exp">Use a reset that expires within (hours)</Field.Label>
		<div class="flex flex-wrap items-center gap-2 text-sm">
			<Input
				id="{idPrefix}-exp"
				type="number"
				min={0}
				class="w-24"
				bind:value={policy.expiringHours}
			/>
			<span class="text-muted-foreground">once a limit is at least</span>
			<Input
				type="number"
				min={0}
				max={100}
				class="w-20"
				aria-label="Minimum usage percent"
				disabled={!Number(policy.expiringHours)}
				bind:value={policy.expiringMinUsedPercent}
			/>
			<span class="text-muted-foreground">% used</span>
		</div>
		<Field.Description>
			So resets about to expire aren't lost. 0 hours turns this off; it ignores the reserve, since
			the reset would be gone anyway.
		</Field.Description>
	</Field.Field>
</Field.Group>
