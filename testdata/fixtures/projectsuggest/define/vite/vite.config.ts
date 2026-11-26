import 'dotenv/config';
import { defineConfig } from 'vite';

// A literal Vite config that loads .env into process.env: its define
// entries are read.
export default defineConfig({
  define: {
    'import.meta.env.VITE_FIXED': JSON.stringify('http://localhost:3100'),
    'import.meta.env.VITE_API': JSON.stringify(process.env.VITE_TARGET),
    __VITE_COMPUTED__: JSON.stringify(`${process.env.HOST}/api`),
  },
});
