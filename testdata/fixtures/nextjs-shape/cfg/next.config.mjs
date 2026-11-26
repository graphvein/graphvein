// A config function is not read: its redirects, rewrites and headers may
// apply to every route (row SH-N24).
export default (phase) => ({
  async redirects() {
    return phase === 'x' ? [{ source: '/api/hello', destination: '/', permanent: false }] : [];
  },
});
