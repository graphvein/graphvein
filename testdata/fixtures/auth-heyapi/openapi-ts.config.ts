import { defineConfig } from '@hey-api/openapi-ts';

export default defineConfig({
  input: './openapi.json',
  output: { path: './client' },
  plugins: ['@hey-api/client-axios'],
});
