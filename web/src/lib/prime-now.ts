import { invalidateAll } from '$app/navigation';
import { toast } from 'svelte-sonner';
import { admin, errorMessage } from './api.js';
import type { Account } from './types.js';

// The loading toast stays up until the run finishes; updates merge into it, so the
// result has to reset the duration or it would never close.
const resultDuration = 4000;

// primeNow starts a session on an account right away and reports the result as a toast.
export async function primeNow(a: Account) {
	const id = toast.loading('Starting a session…', { duration: Number.POSITIVE_INFINITY });
	const opts = { id, duration: resultDuration };
	try {
		const { run } = await admin.primeAccount(a.id);
		await invalidateAll();
		if (run.outcome === 'started' || run.outcome === 'sent') toast.success('Session started', opts);
		else if (run.outcome === 'skipped') toast.info(run.message || 'Skipped', opts);
		else toast.error(run.message || 'Failed', opts);
	} catch (e) {
		toast.error(errorMessage(e), opts);
	}
}
