import tailwindcss from '@tailwindcss/vite';
import adapter from '@sveltejs/adapter-static';
import { sveltekit } from '@sveltejs/kit/vite';
import { defineConfig } from 'vite';

const backend = process.env.MAITAKE_BACKEND ?? 'http://127.0.0.1:8317';

export default defineConfig({
	plugins: [
		tailwindcss(),
		sveltekit({
			compilerOptions: {
				// Force runes mode for the project, except for libraries. Can be removed in svelte 6.
				runes: ({ filename }) =>
					filename.split(/[/\\]/).includes('node_modules') ? undefined : true
			},
			adapter: adapter({ fallback: 'index.html' })
		})
	],
	server: {
		port: 3000,
		strictPort: true,
		proxy: {
			'/api': backend,
			'/healthz': backend,
			'/v1': { target: backend, ws: true },
			'/backend-api': { target: backend, ws: true }
		}
	}
});
