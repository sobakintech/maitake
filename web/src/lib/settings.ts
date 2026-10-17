import { invalidateAll } from '$app/navigation';
import { toast } from 'svelte-sonner';
import { admin, errorMessage, type Settings } from './api.js';
import type { Draft } from './draft.svelte.js';
import { primerNumbers } from './primer.js';
import { numbers } from './redeem.js';

// coerceSettings turns the strings number inputs hand back into numbers, in place.
export function coerceSettings(next: Settings): Settings {
	const pool = next.pool;
	pool.quotaPollMinutes = Number(pool.quotaPollMinutes);
	pool.maxRetries = Number(pool.maxRetries);
	pool.bootstrapSeconds = Number(pool.bootstrapSeconds);
	pool.keepaliveSeconds = Number(pool.keepaliveSeconds);
	numbers(pool.redeem);
	for (const r of pool.errorRules) {
		r.status = Number(r.status) || 0;
		r.cooldownSeconds = Number(r.cooldownSeconds) || 0;
	}
	primerNumbers(pool.primer.schedule);
	for (const p of Object.values(pool.primer.providers ?? {})) primerNumbers(p);
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
	return next;
}

// saveSettings saves a settings draft and adopts what the server stored.
export async function saveSettings(
	draft: Draft<Settings>,
	toastOnSuccess = true
): Promise<boolean> {
	try {
		const { settings } = await admin.saveSettings(coerceSettings(draft.snapshot()));
		draft.value = structuredClone(settings);
		draft.commit();
		if (toastOnSuccess) toast.success('Saved');
		await invalidateAll();
		return true;
	} catch (err) {
		toast.error(errorMessage(err));
		return false;
	}
}
