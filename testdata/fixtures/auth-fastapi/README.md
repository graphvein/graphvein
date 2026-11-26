# auth-fastapi fixture

Rows DA-F1..DA-F3 and DAD of `docs/contract/claims/architecture-graph.md`
(T026, `docs/extract/auth.md`). Hand-authored:

- `app/security.py`: one instance of every `fastapi.security` class, with
  `auto_error` omitted, `False` and non-literal, a literal and a positional
  `scheme_name`, a subclass without and one with `__init__`, and a callable
  instance of a class that is no scheme.
- `app/deps.py`: dependency functions and `Annotated` aliases (nested), the
  `Security` default form, a `Query()` that overrides `Depends`, an unread
  `Annotated` item, a class dependency, an external class dependency
  (`OAuth2PasswordRequestForm`), a method of an instance (unresolved) and a
  chain of nine functions (beyond the depth cap).
- `app/plain.py`: a dependency module that does not import fastapi.
- `app/routers/*`, `app/main.py`, `app/partner.py`: route, router,
  `include_router` and app `dependencies=`, a router included twice with
  different dependencies, a mounted sub-app that starts its own chain.
- `app/unread.py`: forms FastAPI refuses at startup (a cycle, a list item
  that is not `Depends`, `Depends()` without annotation) plus a
  non-literal list and keyword unpacking; the oracle does not import it.

`scripts/oracle/fastapi_security.sh` compares the other apps with FastAPI
0.161.1's own `app.openapi()`. No ignore files or renamed ignored paths are
needed.
