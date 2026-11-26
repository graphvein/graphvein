// Rules that apply before the handlers of the paths they match (row SH-N24).
// The rewrite lists are grown with push(), as many projects do.
const before = [];
before.push({ source: '/api/review/bf', destination: '/api/plain' });
const after = [{ source: '/api/review/dyn/:id', destination: '/api/plain' }];
after.push({ source: '/api/review/st', destination: '/api/plain' });
// Header lists, also grown with push() (rows SH-N24, SH-N30).
const frame = [{ key: 'X-Frame-Options', value: 'DENY' }];
frame.push({ key: 'Cache-Control', value: 'no-store' });
const HEADER = process.env.FIXTURE_HEADER || 'x-fixture';

module.exports = {
  async redirects() {
    return [
      { source: '/api/review/cfg', has: [{ type: 'query', key: 'go' }], destination: '/api/plain', permanent: false },
      { source: '/api/review/gone', destination: '/api/plain', permanent: false },
    ];
  },
  async rewrites() {
    return { beforeFiles: before, afterFiles: [...after] };
  },
  async headers() {
    return [
      { source: '/api/review/cfgh', headers: [{ key: 'x-config', value: '1' }] },
      { source: '/api/review/cfgx', headers: frame },
      { source: '/api/review/cfct', headers: [{ key: 'content-Type', value: 'text/plain; charset=utf-8' }] },
      { source: '/api/review/cfcl', headers: [{ key: 'Content-Length', value: '3' }] },
      { source: '/api/review/cfk', headers: [{ key: HEADER, value: '1' }] },
    ];
  },
};
