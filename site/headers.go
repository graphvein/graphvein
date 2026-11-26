package main

import "strings"

// headers writes Cloudflare Pages' _headers file: a strict content
// security policy (only the site's own files; the GitHub API only with
// the public switch on), the usual security headers, and
// noindex while the switch is off, so the site is not indexed before the
// launch.
func (s *site) headers() string {
	// Scripts: only the site's own files (the screenshot lightbox, and the
	// star count with the switch on); never inline, never third party.
	script, connect := "'self'", "'none'"
	if s.c.Config.PublicRepository {
		connect = "https://api.github.com"
	}
	csp := strings.Join([]string{
		"default-src 'self'",
		"script-src " + script,
		"style-src 'self'",
		"img-src 'self'",
		"font-src 'self'",
		"connect-src " + connect,
		"object-src 'none'",
		"base-uri 'self'",
		"form-action 'none'",
		"frame-ancestors 'none'",
	}, "; ")
	lines := []string{
		"/*",
		"  Content-Security-Policy: " + csp,
		"  X-Content-Type-Options: nosniff",
		"  X-Frame-Options: DENY",
		"  Referrer-Policy: strict-origin-when-cross-origin",
		"  Cross-Origin-Opener-Policy: same-origin",
		"  Permissions-Policy: camera=(), microphone=(), geolocation=(), payment=(), usb=(), interest-cohort=()",
		"  Strict-Transport-Security: max-age=31536000",
	}
	if !s.c.Config.PublicRepository {
		lines = append(lines, "  X-Robots-Tag: noindex")
	}
	return strings.Join(lines, "\n") + "\n"
}
