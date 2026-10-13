// A choice remembered per browser once made; until then callers pass their own default.
export function pref<T extends string>(key: string, values: readonly T[]) {
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
