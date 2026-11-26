import { defineConfig } from 'vite';

// A literal Vite config: every proxy entry is read or refused.
export default defineConfig({
  server: {
    port: 5173,
    proxy: {
      '/api': 'http://localhost:3100',
      "/auth": { target: 'http://localhost:3100', changeOrigin: true },
      '^/regex/.*': 'http://localhost:3100',
      '/rewritten': { target: 'http://localhost:3100', rewrite: (p: string) => p.replace(/^\/rewritten/, '') },
      '/far': 'http://localhost:7000',
      '/variable': { target: process.env.TARGET },
    },
  },
});
