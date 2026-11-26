import { defineConfig } from 'vite';

// Config computed in code: not read.
export default defineConfig(({ mode }) => ({
  server: { proxy: { '/api': mode === 'test' ? 'http://localhost:1' : 'http://localhost:3100' } },
}));
