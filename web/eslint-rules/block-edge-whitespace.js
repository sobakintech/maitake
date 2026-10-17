// Svelte drops whitespace at the start and end of every block body, so in
// `reset{#if at}\n  · {at}{/if}` the space before the dot disappears and it renders as "reset· …".
// Prettier freely moves such text onto its own line, so flag any block body that starts or ends
// with whitespace next to text when the block itself sits right against other inline content.

const parts = {
	SvelteIfBlock: (n) => [n.children],
	SvelteElseBlock: (n) => (n.elseif ? [] : [n.children]),
	SvelteEachBlock: (n) => [n.children],
	SvelteKeyBlock: (n) => [n.children],
	SvelteAwaitPendingBlock: (n) => [n.children],
	SvelteAwaitThenBlock: (n) => [n.children],
	SvelteAwaitCatchBlock: (n) => [n.children]
};

// The block statement as it appears among its siblings, e.g. the `{#if}` for an `{:else}` body.
function outer(node) {
	let n = node;
	while (
		n.type === 'SvelteElseBlock' ||
		(n.type === 'SvelteIfBlock' && n.elseif) ||
		(n.type.startsWith('SvelteAwait') && n.type !== 'SvelteAwaitBlock')
	) {
		n = n.parent;
	}
	return n;
}

const inline = new Set([
	'SvelteText',
	'SvelteMustacheTag',
	'SvelteElement',
	'SvelteRenderTag',
	'SvelteIfBlock',
	'SvelteEachBlock',
	'SvelteKeyBlock',
	'SvelteAwaitBlock'
]);
const isText = (n) => n?.type === 'SvelteText';
const blank = (n) => isText(n) && !/\S/.test(n.value);
const meaningful = (n) =>
	n && n.type !== 'SvelteHTMLComment' && n.type !== 'SvelteConstTag' && !blank(n);
const textLike = (n) => n?.type === 'SvelteText' || n?.type === 'SvelteMustacheTag';

// Whether `n` touches the block with no whitespace in between.
function touches(n, side) {
	if (!n || !inline.has(n.type)) return false;
	if (!isText(n)) return true;
	return side === 'before' ? /\S$/.test(n.value) : /^\S/.test(n.value);
}

export default {
	meta: {
		type: 'problem',
		docs: { description: 'disallow whitespace at the edges of a block body that Svelte will drop' },
		schema: [],
		messages: {
			leading:
				'Svelte drops the whitespace at the start of this block, so it runs into the text before it. Put the separator inside an expression, e.g. {` · ${value}`}.',
			trailing:
				'Svelte drops the whitespace at the end of this block, so it runs into the text after it. Put the separator inside an expression.'
		}
	},
	create(context) {
		const text = context.sourceCode.text;
		// The parser trims text node values, so check the source itself, including the character just
		// outside the node.
		const spaced = (n, side) =>
			side < 0
				? /\s/.test(text[n.range[0] - 1]) || /^\s/.test(text.slice(...n.range))
				: /\s/.test(text[n.range[1]]) || /\s$/.test(text.slice(...n.range));
		function check(node) {
			const block = outer(node);
			const siblings = block.parent?.children ?? block.parent?.body ?? [];
			const i = siblings.indexOf(block);
			const before = siblings[i - 1];
			const after = siblings[i + 1];
			for (const kids of parts[node.type](node)) {
				const first = kids.find(meaningful);
				const last = kids.findLast(meaningful);
				if (!first) continue;
				if (touches(before, 'before') && textLike(first) && spaced(first, -1)) {
					context.report({ node: first, messageId: 'leading' });
				}
				if (touches(after, 'after') && textLike(last) && spaced(last, 1)) {
					context.report({ node: last, messageId: 'trailing' });
				}
			}
		}
		return Object.fromEntries(Object.keys(parts).map((t) => [t, check]));
	}
};
