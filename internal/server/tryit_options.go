package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/graphvein/graphvein/internal/config"
	"github.com/graphvein/graphvein/internal/egress"
)

// This file holds the user-confirmed options of the try-it proxy (T029,
// ADR 0081): free-form paths, multipart bodies, the system proxy
// and CA file of an environment, the certificate of a refused TLS
// connection, and following a redirect once. Each is used only after an
// explicit user action: a guarded edit of the environment, or a one-time
// confirmation bound to the exact request.

// freeFormPath checks and encodes a free-form path (row TI-26): it starts
// with "/", has no query or fragment (query pairs go in query), and every
// byte outside the characters a path segment may hold is percent-encoded
// (a valid %XX escape is kept as written). As for an operation's path, no
// segment may be "." or ".." after percent-decoding, so the request stays
// under the environment's base URL.
func freeFormPath(p string) (string, error) {
	if !strings.HasPrefix(p, "/") {
		return "", badRequest("a free-form path starts with / (it is appended to the environment's base URL)")
	}
	if len(p) > maxTryItValue || strings.ContainsAny(p, "?#\r\n\x00") {
		return "", badRequest("a free-form path is at most %d bytes, without ?, # (query pairs go in query), line breaks or NUL", maxTryItValue)
	}
	const safe = "-._~!$&'()*+,;=:@/"
	var out strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.IndexByte(safe, c) >= 0:
			out.WriteByte(c)
		case c == '%' && i+2 < len(p) && isHex(p[i+1]) && isHex(p[i+2]):
			out.WriteString(p[i : i+3])
			i += 2
		default:
			fmt.Fprintf(&out, "%%%02X", c)
		}
	}
	path := out.String()
	if encodedSeparator(path) {
		return "", &apiError{http.StatusBadRequest, "invalid_path_value", "a free-form path may not hold an encoded / or \\ (%2F, %5C) or a \\: servers that decode them would see other segments"}
	}
	for _, seg := range strings.Split(path, "/") {
		decoded, err := url.PathUnescape(seg)
		if err != nil || decoded == "." || decoded == ".." {
			return "", &apiError{http.StatusBadRequest, "invalid_path_value", "a free-form path may not have a . or .. segment, plain or percent-encoded"}
		}
	}
	return path, nil
}

// encodedSeparator reports a percent-encoded "/" or "\" (any case) in a
// path written by the user or a template: some servers decode them before
// routing, so "..%2f" could climb above the base URL. Values
// filled into a template are encoded by the server and never reach here
// as such.
func encodedSeparator(p string) bool {
	l := strings.ToLower(p)
	return strings.Contains(l, "%2f") || strings.Contains(l, "%5c")
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

// multipartTooLarge is a built multipart body over the request limit.
type multipartTooLarge struct{ n int }

func (e *multipartTooLarge) Error() string { return fmt.Sprintf("multipart body of %d bytes", e.n) }

// buildMultipart builds a multipart/form-data body from parts (row TI-45).
// The boundary is derived from the parts' content, so prepare and send
// build the same bytes and the confirmation hash binds them.
func buildMultipart(parts []partDoc, limit int64) (body []byte, contentType string, summary []partSummary, err error) {
	if len(parts) > maxTryItParts {
		return nil, "", nil, badRequest("at most %d multipart parts", maxTryItParts)
	}
	type part struct {
		name, filename, contentType string
		isFile                      bool
		data                        []byte
	}
	var built []part
	digest := sha256.New()
	size := 0
	for i, p := range parts {
		if p.Name == "" {
			return nil, "", nil, badRequest("multipart part %d needs a name", i+1)
		}
		if err := valueProblem("multipart part name", p.Name); err != nil {
			return nil, "", nil, err
		}
		if (p.Value == nil) == (p.DataBase64 == nil) {
			return nil, "", nil, badRequest("multipart part %s needs exactly one of value and data_base64", p.Name)
		}
		x := part{name: p.Name, contentType: p.ContentType}
		if p.ContentType != "" {
			if err := valueProblem("multipart part content_type", p.ContentType); err != nil {
				return nil, "", nil, err
			}
		}
		if p.Filename != nil {
			if err := valueProblem("multipart part filename", *p.Filename); err != nil {
				return nil, "", nil, err
			}
			x.isFile, x.filename = true, *p.Filename
		}
		if p.Value != nil {
			x.data = []byte(*p.Value)
		} else {
			data, err := base64.StdEncoding.DecodeString(*p.DataBase64)
			if err != nil {
				return nil, "", nil, badRequest("multipart part %s: data_base64 is not standard base64", p.Name)
			}
			x.data = data
		}
		size += len(x.data)
		if int64(size) > limit {
			return nil, "", nil, &multipartTooLarge{size}
		}
		fmt.Fprintf(digest, "%q %q %q %t %d\n", x.name, x.filename, x.contentType, x.isFile, len(x.data))
		digest.Write(x.data)
		built = append(built, x)
	}
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	boundary := "graphvein-" + hex.EncodeToString(digest.Sum(nil))[:40]
	if err := w.SetBoundary(boundary); err != nil {
		return nil, "", nil, fmt.Errorf("setting the multipart boundary: %w", err)
	}
	for _, x := range built {
		if bytes.Contains(x.data, []byte(boundary)) {
			return nil, "", nil, badRequest("multipart part %s contains the boundary; change the content", x.name)
		}
		h := map[string][]string{}
		disposition := fmt.Sprintf(`form-data; name="%s"`, quoteEscaper.Replace(x.name))
		s := partSummary{Name: x.name, Bytes: len(x.data)}
		if x.isFile {
			disposition += fmt.Sprintf(`; filename="%s"`, quoteEscaper.Replace(x.filename))
			if x.contentType == "" {
				x.contentType = "application/octet-stream"
			}
			s.Filename = newPtr(x.filename)
		}
		h["Content-Disposition"] = []string{disposition}
		if x.contentType != "" {
			h["Content-Type"] = []string{x.contentType}
			s.ContentType = newPtr(x.contentType)
		}
		pw, err := w.CreatePart(h)
		if err != nil {
			return nil, "", nil, fmt.Errorf("building the multipart body: %w", err)
		}
		pw.Write(x.data)
		summary = append(summary, s)
	}
	if err := w.Close(); err != nil {
		return nil, "", nil, fmt.Errorf("building the multipart body: %w", err)
	}
	if int64(buf.Len()) > limit {
		return nil, "", nil, &multipartTooLarge{buf.Len()}
	}
	return buf.Bytes(), w.FormDataContentType(), summary, nil
}

// quoteEscaper escapes a form-data name or filename as mime/multipart
// does.
var quoteEscaper = strings.NewReplacer("\\", "\\\\", `"`, "\\\"")

// proxyFor returns the system proxy of b when its environment opted in
// (row TI-43), else nil. No network is used.
func (t *tryItState) proxyFor(b built) (*url.URL, error) {
	if !b.env.Options.UseSystemProxy {
		return nil, nil
	}
	u, err := url.Parse(b.url)
	if err != nil {
		return nil, badRequest("building the request: %v", err)
	}
	proxy, err := egress.SystemProxyFor(u, func(k string) string { v, _ := t.getenv(k); return v })
	if err != nil {
		return nil, &apiError{http.StatusConflict, "invalid_proxy", fmt.Sprintf("environment %s uses the system proxy, but %v", b.env.ID, err)}
	}
	return proxy, nil
}

// loadCA reads e's tls.ca_file (row TI-42) when the environment is
// resolved for a request, so its content is part of the environment's key
// (a changed bundle is a changed destination, row TI-25) and the send uses
// exactly the bytes the key was made from. A relative path is read from
// the directory of the file that configures it; the file must be a
// regular file of at most maxCAFile bytes, read within the environment's
// time limit. A failure is kept and refuses the send.
func loadCA(e *tryEnv) {
	if e.Options.TLS == nil || e.Options.TLS.CAFile == "" {
		return
	}
	path := e.Options.TLS.CAFile
	if !filepath.IsAbs(path) {
		path = filepath.Join(e.Dir, filepath.FromSlash(path))
	}
	timeout := config.DefaultTimeoutSeconds
	if l := e.Options.Limits.TimeoutSeconds; l != 0 {
		timeout = l
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()
	data, err := readCAFile(ctx, path)
	if err != nil {
		e.caErr, e.caHash = err, "unreadable"
		return
	}
	sum := sha256.Sum256(data)
	e.caPEM, e.caHash = data, hex.EncodeToString(sum[:])
}

// readCAFile reads a regular file of at most maxCAFile bytes, giving up
// when ctx ends (a file on a stalled network share, a device).
func readCAFile(ctx context.Context, path string) ([]byte, error) {
	type result struct {
		data []byte
		err  error
	}
	done := make(chan result, 1)
	go func() {
		info, err := os.Stat(path)
		switch {
		case err != nil:
			done <- result{nil, err}
			return
		case !info.Mode().IsRegular():
			done <- result{nil, errors.New("not a regular file")}
			return
		case info.Size() > maxCAFile:
			done <- result{nil, fmt.Errorf("larger than %d MiB", maxCAFile>>20)}
			return
		}
		f, err := os.Open(path)
		if err != nil {
			done <- result{nil, err}
			return
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, maxCAFile+1))
		if err == nil && len(data) > maxCAFile {
			err = fmt.Errorf("larger than %d MiB", maxCAFile>>20)
		}
		done <- result{data, err}
	}()
	select {
	case r := <-done:
		return r.data, r.err
	case <-ctx.Done():
		return nil, errors.New("reading it took longer than the environment's time limit")
	}
}

// caFile returns the CA bundle loadCA read for e, or the refusal.
func (t *tryItState) caFile(e tryEnv) ([]byte, error) {
	if e.caErr != nil {
		return nil, &apiError{http.StatusConflict, "ca_file_error", fmt.Sprintf("environment %s: reading tls.ca_file %s: %v", e.ID, e.Options.TLS.CAFile, e.caErr)}
	}
	return e.caPEM, nil
}

// redirectFollow describes how the user can follow a redirect once (row
// TI-46), or nil for a Location that is not an http(s) URL with a host.
func (t *tryItState) redirectFollow(b built, repository string, status int, location string) *redirectFollowDoc {
	method, keepsBody := b.method, true
	switch status {
	case http.StatusMovedPermanently, http.StatusFound:
		if method == http.MethodPost {
			method, keepsBody = http.MethodGet, false
		}
	case http.StatusSeeOther:
		if method != http.MethodHead {
			method = http.MethodGet
		}
		keepsBody = false
	case http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
	default:
		return nil
	}
	base, err := url.Parse(b.url)
	if err != nil {
		return nil
	}
	target, err := base.Parse(location)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" {
		return nil
	}
	origin, err := config.ParseBaseURL(target.Scheme + "://" + target.Host)
	if err != nil {
		return nil
	}
	path := target.EscapedPath()
	if path == "" {
		path = "/"
	}
	var query []pairDoc
	if target.RawQuery != "" {
		for _, kv := range strings.Split(target.RawQuery, "&") {
			name, value, _ := strings.Cut(kv, "=")
			n, err1 := url.QueryUnescape(name)
			v, err2 := url.QueryUnescape(value)
			if err1 != nil || err2 != nil {
				return nil
			}
			if n != "" {
				query = append(query, pairDoc{n, v})
			}
		}
	}
	d := &redirectFollowDoc{Origin: origin.Origin(), SameOrigin: origin.Origin() == b.env.Base.Origin(), Method: method, KeepsBody: keepsBody && b.body != nil, Path: path, Query: query}
	target.User, target.Fragment, target.RawFragment = nil, "", ""
	d.URL = target.String()
	// The environment whose base URL holds the target, longest base path
	// first.
	var match *tryEnv
	names := map[string]bool{}
	if s, err := t.scope(repository); err == nil {
		for i, e := range s.envs {
			names[e.Name] = true
			if e.Base.Origin() != d.Origin || !(e.Base.Path == "" || path == e.Base.Path || strings.HasPrefix(path, e.Base.Path+"/")) {
				continue
			}
			if match == nil || len(e.Base.Path) > len(match.Base.Path) {
				match = &s.envs[i]
			}
		}
	}
	if match != nil {
		d.Environment = newPtr(match.ID)
		if d.Path = strings.TrimPrefix(path, match.Base.Path); d.Path == "" {
			d.Path = "/"
		}
		return d
	}
	name := environmentNameFor(origin)
	for i := 2; names[name]; i++ {
		name = fmt.Sprintf("%s-%d", environmentNameFor(origin), i)
	}
	d.SuggestedEnvironment = &suggestedEnvDoc{Name: name, BaseURL: origin.Origin()}
	return d
}

// environmentNameFor makes a valid environment name from an origin's host
// and port.
func environmentNameFor(b config.BaseURL) string {
	var out strings.Builder
	for _, c := range b.Host + "-" + fmt.Sprint(b.Port) {
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' {
			out.WriteRune(c)
		} else {
			out.WriteByte('-')
		}
	}
	name := out.String()
	if len(name) > 56 {
		name = name[:56]
	}
	return name
}

// certificateDoc describes a server certificate for the user's decision
// to pin it (row TI-29): never trusted from this description alone.
type certificateDoc struct {
	Subject   string   `json:"subject"`
	Issuer    string   `json:"issuer"`
	SANs      []string `json:"sans"`
	NotBefore string   `json:"not_before"`
	NotAfter  string   `json:"not_after"`
	// SHA256 is the lower-case hex SHA-256 of the certificate's DER, the
	// value a pin takes.
	SHA256     string `json:"sha256"`
	SelfSigned bool   `json:"self_signed"`
}

// certificateDetails is the details object of a certificate refusal.
type certificateDetails struct {
	Environment string         `json:"environment"`
	Origin      string         `json:"origin"`
	Certificate certificateDoc `json:"certificate"`
	// Reason is why normal verification failed.
	Reason string `json:"reason"`
	// PinnedSHA256 are the pins configured for the environment: non-empty
	// when the certificate changed since one was pinned.
	PinnedSHA256 []string `json:"pinned_sha256"`
}

func newCertificateDetails(ce *egress.CertificateError, e tryEnv) certificateDetails {
	leaf := ce.Leaf
	sans := []string{}
	sans = append(sans, leaf.DNSNames...)
	for _, ip := range leaf.IPAddresses {
		sans = append(sans, ip.String())
	}
	for _, u := range leaf.URIs {
		sans = append(sans, u.String())
	}
	sans = append(sans, leaf.EmailAddresses...)
	selfSigned := bytes.Equal(leaf.RawIssuer, leaf.RawSubject) && leaf.CheckSignature(leaf.SignatureAlgorithm, leaf.RawTBSCertificate, leaf.Signature) == nil
	return certificateDetails{Environment: e.ID, Origin: e.Base.Origin(), Reason: ce.Err.Error(), PinnedSHA256: append([]string{}, ce.Pinned...),
		Certificate: certificateDoc{Subject: leaf.Subject.String(), Issuer: leaf.Issuer.String(), SANs: sans,
			NotBefore: leaf.NotBefore.UTC().Format(time.RFC3339), NotAfter: leaf.NotAfter.UTC().Format(time.RFC3339), SHA256: ce.Fingerprint, SelfSigned: selfSigned}}
}

func certificateMessage(ce *egress.CertificateError, e tryEnv) string {
	if len(ce.Pinned) > 0 {
		return fmt.Sprintf("the certificate of %s changed: it is %s, environment %s pins %s; nothing was sent. Check it, then pin the new certificate or remove the pin", e.Base.Origin(), ce.Fingerprint, e.ID, strings.Join(ce.Pinned, ", "))
	}
	return fmt.Sprintf("the certificate of %s is not trusted (%v); nothing was sent. Check its fingerprint %s, then pin it for environment %s, or add its CA as tls.ca_file", e.Base.Origin(), ce.Err, ce.Fingerprint, e.ID)
}

// redirectFollowDoc is a redirect's target as a request the user can send
// once (row TI-46): through an existing environment of this repository
// whose base URL holds the target, or, when none does, after adding the
// target's origin as an environment (SuggestedEnvironment) through the
// guarded edit. It is sent as a free-form request, so it needs its own
// confirmation; nothing is followed automatically.
type redirectFollowDoc struct {
	// URL is the target resolved against the request URL, without user
	// info or fragment.
	URL        string `json:"url"`
	Origin     string `json:"origin"`
	SameOrigin bool   `json:"same_origin"`
	// Method is the method a browser would use for the status (303, and
	// POST on 301/302, become GET; 307/308 keep the method and body).
	Method    string `json:"method"`
	KeepsBody bool   `json:"keeps_body"`
	// Environment is the id of the environment to send it to, or null;
	// Path is relative to that environment's base URL (to the origin when
	// null) and Query its query pairs, as free_form path and query.
	Environment          *string          `json:"environment"`
	Path                 string           `json:"path"`
	Query                []pairDoc        `json:"query"`
	SuggestedEnvironment *suggestedEnvDoc `json:"suggested_environment"`
}

type suggestedEnvDoc struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
}
