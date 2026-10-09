// Account emails are blurred until hovered, unless the viewer reveals them all. Remembered per browser.
const key = 'maitake:reveal-private';

let revealed = $state(typeof localStorage !== 'undefined' && localStorage.getItem(key) === '1');

export const privacy = {
	get revealed() {
		return revealed;
	},
	toggle() {
		revealed = !revealed;
		localStorage.setItem(key, revealed ? '1' : '0');
	}
};
