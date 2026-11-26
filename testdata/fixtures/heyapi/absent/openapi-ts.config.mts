import { defineConfig } from '@hey-api/openapi-ts';
export default defineConfig({ output: './missing', plugins: [{ name: '@hey-api/client-axios' }] });
