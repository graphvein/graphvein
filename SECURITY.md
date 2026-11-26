# Security policy

## Supported versions

Graphvein is in preview. Only the latest preview release receives security
fixes; older previews do not. Fixes land on `main` first and ship in the
next preview release.

| Version | Supported |
|---|---|
| Latest preview release | yes |
| Older preview releases | no |
| Unreleased `main` | best effort |

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub's private
vulnerability reporting: open the repository's **Security** tab and choose
**Report a vulnerability** (a GitHub security advisory draft). Do not open a
public issue, discussion or pull request for a suspected vulnerability.
If you cannot use the form, email contact@graphvein.com.

Include what you can of:

- the affected version or commit and the operating system;
- the component (indexer, local server and web UI, "try it" proxy, store,
  CLI);
- steps to reproduce, a proof of concept, and the impact you expect.

You can expect an acknowledgement within a few working days, and an
assessment with a plan for a fix or a reason for declining. Please give us
reasonable time to release a fix before you disclose details publicly; we
will credit you in the advisory unless you prefer otherwise.

## Scope notes

Graphvein runs locally. By default the server binds to `127.0.0.1` only,
outbound network access goes through a single package with an allow-list
and an offline mode, and "try it" requests go through the server's guarded
proxy. Reports that show a way around any of these are especially welcome.

## Conduct reports

Reports under the [code of conduct](CODE_OF_CONDUCT.md) go privately to
the project owner at contact@graphvein.com. You can also use GitHub's
private vulnerability reporting form above and start the title with
"Conduct:"; the report is visible to the maintainers only.
