import { invalidateAll } from '$app/navigation';
import { toast } from 'svelte-sonner';
import { admin, errorMessage } from './api.js';
import type { Account } from './types.js';

// primeNow starts a session on an account right away and reports the result as a toast.
export async function primeNow(a: Account) {
	const id = toast.loading(`Starting a session on ${a.label}…`);
	try {
		const { run } = await admin.primeAccount(a.id);
		const show =
			run.outcome === 'started' || run.outcome === 'sent'
				? toast.success
				: run.outcome === 'skipped'
					? toast.info
					: toast.error;
		show(run.message || 'Done', { id });
		await invalidateAll();
	} catch (e) {
		toast.error(errorMessage(e), { id });
	}
}
