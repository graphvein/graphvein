import { defineConfig } from '@hey-api/openapi-ts';
export default defineConfig({ output: { path: './generated' }, plugins: ['@hey-api/client-fetch'] });
