package typescript

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/graphvein/graphvein/internal/parse"
)

func nextFacts(t *testing.T, file, source string) Facts {
	t.Helper()
	results, err := parse.Batch(context.Background(), []parse.Job{{Language: "typescript", Source: []byte(source)}}, parse.Options{CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil || results[0].Err != nil {
		t.Fatal(results, err)
	}
	return Extract(file, []byte(source), results[0].Root)
}

// TestNextConfigReading covers the literal reading of next.config files
// (rows NX-P2, NX-P3): only literal values are read, everything else is
// unread, never guessed.
func TestNextConfigReading(t *testing.T) {
	cases := []struct{ source, want string }{
		{"const nextConfig = { basePath: '/docs', trailingSlash: true, pageExtensions: ['page.tsx', 'ts'] }\nexport default nextConfig\n",
			`{"BasePath":"/docs","BasePathState":"literal","TrailingSlash":true,"TrailingSlashState":"literal","PageExtensions":["page.tsx","ts"],"PageExtensionsState":"literal","RewritesState":"absent","Shape":"object"}`},
		{"module.exports = withNextIntl({ basePath: process.env.BASE_PATH, async rewrites() { return [{ source: '/old/:path*', destination: '/api/new/:path*' }] } })\n",
			`{"BasePathState":"unread","TrailingSlashState":"absent","PageExtensionsState":"absent","Rewrites":[{"phase":"afterFiles","source":"/old/:path*","destination":"/api/new/:path*"}],"RewritesState":"literal","Rules":[{"key":"rewrites","phase":"afterFiles","source":"/old/:path*"}],"Shape":"object"}`},
		{"export default { rewrites: async () => ({ beforeFiles: [{ source: '/a', destination: '/b' }], fallback: [] }) }\n",
			`{"BasePathState":"absent","TrailingSlashState":"absent","PageExtensionsState":"absent","Rewrites":[{"phase":"beforeFiles","source":"/a","destination":"/b"}],"RewritesState":"literal","Rules":[{"key":"rewrites","phase":"beforeFiles","source":"/a"}],"Shape":"object"}`},
		{"const rewrites = []\nrewrites.push({ source: '/a', destination: '/b' })\nexport default { rewrites: async () => rewrites }\n",
			`{"BasePathState":"absent","TrailingSlashState":"absent","PageExtensionsState":"absent","RewritesState":"unread","Rules":[{"key":"rewrites","phase":"afterFiles","source":"/a"}],"Shape":"object"}`},
		{"export default { async rewrites() { return [{ source: '/a/(.*)', destination: '/b' }] } }\n",
			`{"BasePathState":"absent","TrailingSlashState":"absent","PageExtensionsState":"absent","RewritesState":"unread","Rules":[{"key":"rewrites","phase":"afterFiles","source":"/a/(.*)"}],"Shape":"object"}`},
		{"const config = { basePath: '/x' }\nconfig.basePath = '/y'\nexport default config\n",
			`{"BasePathState":"unread","TrailingSlashState":"unread","PageExtensionsState":"unread","RewritesState":"unread","Shape":"unread"}`},
		{"export default (phase) => ({ basePath: '/x' })\n",
			`{"BasePathState":"unread","TrailingSlashState":"unread","PageExtensionsState":"unread","RewritesState":"unread","Shape":"unread"}`},
		{"const base = { basePath: '/x' }\nexport default { ...base, trailingSlash: false }\n",
			`{"BasePathState":"unread","TrailingSlashState":"literal","PageExtensionsState":"unread","RewritesState":"unread","RulesUnread":["redirects","rewrites","headers"],"Shape":"object"}`},
		{"module.exports = withA(withB({ reactStrictMode: true }))\n",
			`{"BasePathState":"absent","TrailingSlashState":"absent","PageExtensionsState":"absent","RewritesState":"absent","Shape":"object"}`},
		{"const basePath = process.env.X ? '/demo' : ''\nconst c = { ...(basePath ? { basePath, assetPrefix: '/a' } : {}), trailingSlash: true }\nexport default withB(c)\n",
			`{"BasePathState":"unread","TrailingSlash":true,"TrailingSlashState":"literal","PageExtensionsState":"absent","RewritesState":"absent","Shape":"object"}`},
		// redirects and headers (T029 SH-N24): literal sources only.
		{"export default { async redirects() { return [{ source: '/old', destination: '/', permanent: false, has: [{ type: 'query', key: 'x' }] }] }, headers: async () => [{ source: '/api/:path*', headers: [{ key: 'x-a', value: '1' }] }] }\n",
			`{"BasePathState":"absent","TrailingSlashState":"absent","PageExtensionsState":"absent","RewritesState":"absent","Rules":[{"key":"redirects","source":"/old","conditional":true},{"key":"headers","source":"/api/:path*","header_keys":["x-a"],"header_keys_read":true}],"Shape":"object"}`},
		{"export default { redirects: buildRedirects, async headers() { return [{ source: '/a', basePath: false, headers: [] }] } }\n",
			`{"BasePathState":"absent","TrailingSlashState":"absent","PageExtensionsState":"absent","RewritesState":"absent","Rules":[{"key":"headers","header_keys_read":true}],"RulesUnread":["redirects"],"Shape":"object"}`},
		{"const rules = [{ source: '/a', destination: '/b', permanent: true }]\nexport default { async redirects() { return rules } }\n",
			`{"BasePathState":"absent","TrailingSlashState":"absent","PageExtensionsState":"absent","RewritesState":"absent","Rules":[{"key":"redirects","source":"/a"}],"Shape":"object"}`},
		// Rule lists grown with push(), spread and concat (T029 SH-N24).
		{"const S = '/script.js'\nconst rules = [{ source: '/api/:path*', headers: [] }]\nif (x) { rules.push({ source: S, headers: [] }) }\nconst more = [{ source: `/${name}`, headers: [] }]\nexport default { async headers() { return [...rules].concat(more, { source: '/z', missing: [] }) } }\n",
			`{"BasePathState":"absent","TrailingSlashState":"absent","PageExtensionsState":"absent","RewritesState":"absent","Rules":[{"key":"headers","source":"/api/:path*","header_keys_read":true},{"key":"headers","source":"/script.js","header_keys_read":true},{"key":"headers","header_keys_read":true},{"key":"headers","source":"/z","conditional":true}],"Shape":"object"}`},
		// Header names of headers rules (T029 NXH): literal keys of lists
		// grown with push() and shared by rules; a key that is not a
		// literal token, an element or member that is not read, a spread
		// after the list: not read.
		{"const CT = 'content-TYPE'\nconst api = [{ key: 'Access-Control-Allow-Origin', value: '*' }]\nif (x) { api.push({ key: 'Cache-Control', value: 'no-cache' }) }\nexport default { async headers() { return [{ source: '/api/:path*', headers: api }, { source: '/:path*', headers: [...api, { key: CT, value: 'text/plain' }] }, { ...base, source: '/s', headers: api }] } }\n",
			`{"BasePathState":"absent","TrailingSlashState":"absent","PageExtensionsState":"absent","RewritesState":"absent","Rules":[{"key":"headers","source":"/api/:path*","header_keys":["Access-Control-Allow-Origin","Cache-Control"],"header_keys_read":true},{"key":"headers","source":"/:path*","header_keys":["Access-Control-Allow-Origin","Cache-Control","content-TYPE"],"header_keys_read":true},{"key":"headers","conditional":true,"header_keys":["Access-Control-Allow-Origin","Cache-Control"],"header_keys_read":true}],"Shape":"object"}`},
		{"export default { async headers() { return [{ source: '/a/:h', headers: [{ key: 'x-:h', value: '1' }] }, { source: '/b', headers: [{ key: process.env.H, value: '1' }] }, { source: '/c', headers: [{ ...h, key: 'x-c' }] }, { source: '/d', headers: [{ value: '1' }] }, { source: '/e', headers: list() }, { source: '/f', headers: [{ key: 'x-f', value: '1' }], ...more }, { source: '/g', headers: [{ key: 'x-g', value: '1' }], [k]: [] }] } }\n",
			`{"BasePathState":"absent","TrailingSlashState":"absent","PageExtensionsState":"absent","RewritesState":"absent","Rules":[{"key":"headers","source":"/a/:h"},{"key":"headers","source":"/b"},{"key":"headers","source":"/c"},{"key":"headers","source":"/d"},{"key":"headers","source":"/e"},{"key":"headers","conditional":true},{"key":"headers","source":"/g"}],"Shape":"object"}`},
		{"const rules = []\nrules.splice(0, 0, { source: '/a', destination: '/b' })\nexport default { rewrites: async () => rules }\n",
			`{"BasePathState":"absent","TrailingSlashState":"absent","PageExtensionsState":"absent","RewritesState":"unread","RulesUnread":["rewrites"],"Shape":"object"}`},
	}
	for i, c := range cases {
		f := nextFacts(t, "web/next.config.mjs", c.source)
		if f.Next == nil || f.Next.Config == nil {
			t.Fatalf("case %d: no config read", i)
		}
		got, _ := json.Marshal(f.Next.Config)
		if string(got) != c.want {
			t.Errorf("case %d:\n got %s\nwant %s", i, got, c.want)
		}
	}
	if f := nextFacts(t, "web/other.config.mjs", "export default {}\n"); f.Next == nil || f.Next.Config != nil || f.Next.DefaultExport == nil {
		t.Errorf("a file not named next.config.* got %+v", f.Next)
	}
}

// TestDestructuredExports keeps the names a destructuring export binds.
func TestDestructuredExports(t *testing.T) {
	f := nextFacts(t, "a.ts", "export const { a, b: { GET }, c = 1, ...rest } = x\nexport const [POST] = y\nconst { z } = w\n")
	var names []string
	for _, n := range f.Next.DestructuredExports {
		names = append(names, n.Name)
	}
	got, _ := json.Marshal(names)
	if string(got) != `["a","GET","c","rest","POST"]` {
		t.Fatal(string(got))
	}
}
