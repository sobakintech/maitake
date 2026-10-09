// Draft tracks an editable copy of server state so pages can tell when there are unsaved changes.
export class Draft<T> {
	base = $state.raw() as T;
	value = $state() as T;

	constructor(initial: T) {
		this.base = initial;
		this.value = structuredClone(initial);
	}

	dirty = $derived(JSON.stringify(this.value) !== JSON.stringify(this.base));

	reset() {
		this.value = structuredClone(this.base);
	}

	// sync adopts fresh server data without discarding edits in progress.
	sync(next: T) {
		const keepEdits = this.dirty;
		this.base = next;
		if (!keepEdits) this.value = structuredClone(next);
	}

	// commit marks the current value as saved.
	commit() {
		this.base = $state.snapshot(this.value) as T;
	}

	snapshot(): T {
		return $state.snapshot(this.value) as T;
	}
}
