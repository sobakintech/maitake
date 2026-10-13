import { pref } from './pref.svelte.js';

// Whether quota meters show what's left or what's used; remembered per browser once flipped.
export type QuotaMode = 'left' | 'used';

export const quotaMode = pref<QuotaMode>('maitake:quota-mode', ['left', 'used']);
