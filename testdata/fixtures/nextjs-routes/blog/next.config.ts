import type { NextConfig } from 'next';

const config: NextConfig = {
  basePath: '/blog',
  trailingSlash: true,
  pageExtensions: ['page.tsx', 'ts'],
};

export default config;
