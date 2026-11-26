## What this changes

<!-- One or two sentences: what and why. Link the issue it addresses. -->

## Checklist

- [ ] I have signed the contributor licence agreement (CLA Assistant asks on your first pull request).
- [ ] `bash scripts/test.sh` passes locally (Postgres subtests run in CI).
- [ ] New behaviour has a test; extractor or linker changes have a fixture with a golden output.
- [ ] No golden file was updated just to make a test pass; every golden change is explained below.
- [ ] Claim contract tables, docs and code tables changed together, or no contract changed.
- [ ] `bash scripts/check-web.sh` passes (only if `web/` changed).
- [ ] New dependencies have allowed licences and `THIRD_PARTY_NOTICES` is regenerated (only if dependencies changed).
- [ ] `git status --porcelain --ignored` and `git ls-files -ci --exclude-standard` list no file I added.
- [ ] An ADR (`**Status:** Proposed`) records any architecture, contract, schema, dependency or public interface decision.

## Golden changes

<!-- Which goldens changed and exactly why, or "none". -->

## Follow-ups

<!-- Anything you noticed but did not change, or "none". -->
