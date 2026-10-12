<script lang="ts">
	import type { PrimerPolicy, PrimerSchedule } from '#lib/types.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import TimezonePicker from './timezone-picker.svelte';
	import WeekdayPicker from './weekday-picker.svelte';
	import { clock, minutes, overlaps, pingTime } from '#lib/primer.js';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import XIcon from '@lucide/svelte/icons/x';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';

	// Edits a session primer policy: schedules of reset times, and the backup ping.
	let {
		policy = $bindable(),
		serverTimezone,
		idPrefix = 'primer',
		sessionMinutes = 300
	}: {
		policy: PrimerPolicy;
		serverTimezone: string;
		idPrefix?: string;
		sessionMinutes?: number;
	} = $props();

	const serverZone = $derived(serverTimezone ? `Server time (${serverTimezone})` : 'Server time');

	// A new time follows the last one by a session, so its ping lands right as that session ends.
	function addTime(s: PrimerSchedule) {
		const last = s.times.at(-1);
		s.times = [...s.times, last ? clock(minutes(last) + sessionMinutes) : '09:00'];
	}

	function addSchedule() {
		policy.schedules = [...policy.schedules, { times: ['09:00'], days: [] }];
	}

	function setBackup(on: boolean) {
		policy.retries = on ? 1 : 0;
		if (on) {
			policy.verify = true;
			policy.retrySeconds = 60;
		}
	}
</script>

<Field.Group>
	<div class="space-y-3">
		{#each policy.schedules as schedule, i (i)}
			<div class="space-y-4 rounded-lg border p-4">
				<div class="flex items-center justify-between gap-2">
					<span class="text-sm font-medium">Schedule {i + 1}</span>
					<Button
						variant="ghost"
						size="icon-sm"
						class="text-muted-foreground"
						aria-label="Remove schedule {i + 1}"
						onclick={() => (policy.schedules = policy.schedules.filter((_, j) => j !== i))}
					>
						<Trash2Icon />
					</Button>
				</div>

				<Field.Set>
					<Field.Legend variant="label">Reset at</Field.Legend>
					<div class="space-y-2">
						{#each schedule.times, t}
							{@const p = pingTime(schedule.times[t], sessionMinutes)}
							<div class="flex items-center gap-2">
								<Input
									type="time"
									class="w-28"
									aria-label="Reset at"
									bind:value={schedule.times[t]}
								/>
								<span class="text-xs text-muted-foreground tabular-nums">
									pings at {p.ping}{p.dayBefore ? ' the day before' : ''}
								</span>
								<Button
									variant="ghost"
									size="icon-sm"
									class="text-muted-foreground"
									aria-label="Remove time"
									onclick={() => (schedule.times = schedule.times.filter((_, j) => j !== t))}
								>
									<XIcon />
								</Button>
							</div>
						{/each}
						{#each overlaps(schedule.times, sessionMinutes) as w (w)}
							<p class="flex items-start gap-2 text-xs text-muted-foreground">
								<TriangleAlertIcon
									class="mt-0.5 size-3.5 shrink-0"
									style="color: var(--viz-warning)"
								/>
								{w}
							</p>
						{/each}
						<Button size="sm" variant="outline" onclick={() => addTime(schedule)}>
							<PlusIcon /> Add a time
						</Button>
					</div>
				</Field.Set>

				<div class="flex flex-wrap gap-x-6 gap-y-4">
					<Field.Field class="w-auto">
						<Field.Label>Days</Field.Label>
						<WeekdayPicker bind:days={schedule.days} />
					</Field.Field>
					<Field.Field class="w-auto">
						<Field.Label for="{idPrefix}-tz-{i}">Time zone</Field.Label>
						<TimezonePicker
							id="{idPrefix}-tz-{i}"
							class="w-72"
							bind:value={
								() => schedule.timezone ?? '', (v) => (schedule.timezone = v || undefined)
							}
							defaultLabel={serverZone}
						/>
					</Field.Field>
				</div>
			</div>
		{/each}
		<Button size="sm" variant="outline" onclick={addSchedule}>
			<PlusIcon /> Add a schedule
		</Button>
	</div>

	<Field.Field orientation="horizontal">
		<Field.Content>
			<Field.Label for="{idPrefix}-backup">Backup ping</Field.Label>
			<Field.Description>Pings again a minute later if the session didn't start.</Field.Description>
		</Field.Content>
		<Switch id="{idPrefix}-backup" checked={policy.retries > 0} onCheckedChange={setBackup} />
	</Field.Field>
</Field.Group>
