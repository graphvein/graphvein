# route-model-tables fixture

Rows MT-20..MT-29 of `docs/contract/claims/route-model-types.md` (routes
that accept or return a database model; the schema view's table detail and
the route detail). Hand-authored FastAPI and SQLModel code:

- `app/models.py`: the SQLModel tables `Hero`, `Team` and `Audit`
  (`table=True`), the models `HeroBase`, `HeroPublic` and `TeamList`
  (`data: list[Team]`), and `HeroCopy`, which declares the `Hero` table's
  fields again.
- `app/other.py`: a Pydantic class also named `Hero`.
- `app/main.py`: the routes:
  - `POST /heroes`: the `Hero` table class as the body, `HeroPublic` as
    the response (MT-21);
  - `GET /heroes/{hero_id}`: returns `Hero` (MT-23);
  - `GET /heroes`: returns `list[Hero]` (MT-23 through an array);
  - `GET /copies/{copy_id}`: returns `HeroCopy`, the same fields as the
    table: never linked (MT-29);
  - `GET /others/{other_id}`: returns the other file's `Hero`, the same
    name: never linked (MT-29);
  - `GET /teams`: returns `TeamList`, whose field holds `Team` (MT-24,
    nested);
  - `PUT /teams/{team_id}`: `Team` as the body and `Team | None` as the
    response.
- `Audit` is used by no route: its table detail says that no route whose
  shape was read uses it, with the count of routes whose request or
  response is not fully read (MT-26).

The fixture has no ignore files and no ignored paths.
