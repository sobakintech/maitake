// Whether quota meters show what's left or what's used; remembered per browser once flipped.
export type QuotaMode = 'left' | 'used';

function pref<T extends string>(key: string, values: readonly T[]) {
	const load = (): T | null => {
		if (typeof localStorage === 'undefined') return null;
		const v = localStorage.getItem(key) as T | null;
		return v && values.includes(v) ? v : null;
	};
	let chosen = $state<T | null>(load());
	return {
		get(fallback: T): T {
			return chosen ?? fallback;
		},
		set(v: T) {
			chosen = v;
			localStorage.setItem(key, v);
		}
	};
}

export const quotaMode = pref<QuotaMode>('maitake:quota-mode', ['left', 'used']);
