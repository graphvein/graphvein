"""FastAPI route shape runtime oracle (T029).

Drives the route-shape-fastapi fixture app (a scratch copy, never corpus
code) with FastAPI's TestClient and checks Graphvein's stored claims
against what the running app does:

- every declared query, header, cookie and path parameter claimed
  required is refused with 422 (loc naming it) when it is left out of an
  otherwise complete request; every one claimed optional is not;
- every status Graphvein states as fact (success, raised HTTPException,
  422) is reached by a hand-written request in CASES; a fact status no
  case reaches is reported as not driven;
- the routes whose success response Graphvein lowered to intended
  (a Response returned directly or a Response parameter changed) do
  return another status on some request;
- every fact 422 is reached (by a case or by a generated invalid request:
  a required parameter left out, a typed value replaced by "zz", a
  malformed JSON body);
- every fact success response with a stated schema: the body of a driven
  success request validates against it (required fields present, no
  field outside a model's fields, types);
- every fact request body with a stated schema: a request carrying
  exactly the claimed required fields (under the claimed names) is not
  refused with 422, and leaving out each claimed required field is;
- statuses lowered by project exception handlers or a custom route class
  (rows SH-F37, SH-F38) are never sent by the fixture's cases.

--fixture names the case set: "main" (route-shape-fastapi), "handlers"
(route-shape-fastapi-handlers) or "unpack" (route-shape-fastapi-unpack:
response defaults inherited from objects constructed or included with
keyword unpacking or default_response_class, row SH-F24, and exception
handlers an unpacked FastAPI may pass, row SH-F37) or "assign"
(route-shape-fastapi-assign: default_response_class assignments).

Run by scripts/oracle/fastapi_shape.sh with -I -B. Prints counts and every
disagreement; exits 1 on a disagreement.
"""
import argparse
import json
import sys

# (method, path, request keyword arguments, expected status)
CASES = [
    ("GET", "/items/1", {}, 200),
    ("GET", "/items/0", {}, 404),
    ("GET", "/items/x", {}, 422),
    ("POST", "/items", {"json": {"name": "a", "note": None}}, 201),
    ("POST", "/items", {"json": {}}, 422),
    ("DELETE", "/items/3", {}, 204),
    ("DELETE", "/items/0", {}, 422),
    ("GET", "/returned-direct/-1", {}, 404),
    ("GET", "/returned-direct/x", {}, 422),
    ("GET", "/guarded/-1", {}, 404),
    ("GET", "/guarded/x", {}, 422),
    ("GET", "/try/-1", {}, 400),
    ("GET", "/try/x", {}, 422),
    ("GET", "/reader", {"cookies": {"session": "s"}}, 401),
    ("GET", "/reader", {"headers": {"Authorization": "t"}, "cookies": {"session": "s"}}, 200),
    ("PUT", "/owners/ab", {"json": {"userName": "u", "email": "e"}}, 202),
    ("PUT", "/owners/ab", {"json": {"login": "u", "email": "e"}}, 422),
    ("POST", "/direct", {}, 201),
    ("POST", "/direct", {"params": {"flag": "x"}}, 422),
    ("POST", "/direct", {"params": {"flag": "true"}}, 202),
    ("GET", "/status-param", {}, 200),
    ("GET", "/status-param", {"params": {"slow": "x"}}, 422),
    ("GET", "/status-param", {"params": {"slow": "true"}}, 203),
    ("GET", "/caught/7", {}, 200),
    ("POST", "/login", {"data": {"username": "u", "password": "p"}}, 200),
    ("POST", "/login", {"data": {"username": "u"}}, 422),
    ("POST", "/form", {"data": {"name": "n"}}, 200),
    ("POST", "/form", {"data": {}}, 422),
    ("POST", "/upload", {"files": {"file": ("a.txt", b"x")}, "data": {"title": "t"}}, 200),
    ("POST", "/upload", {"data": {"title": "t"}}, 422),
    ("POST", "/embed", {"json": {"item": {"name": "a", "note": None, "id": 1}}}, 200),
    ("POST", "/embed", {"json": {"name": "a", "note": None, "id": 1}}, 422),
    ("POST", "/multi", {"json": {"item": {"name": "a", "note": None, "id": 1}, "other": {"name": "b", "note": None}, "importance": 1}}, 200),
    ("POST", "/multi", {"json": {}}, 422),
    ("POST", "/ids", {"json": [1, 2]}, 200),
    ("POST", "/ids", {"json": ["x"]}, 422),
    ("POST", "/request-model", {"json": {"name": "a", "note": None}}, 200),
    ("POST", "/request-model", {"json": {"name": "a"}}, 422),
    ("POST", "/checked", {"json": {"name": "a", "note": None}}, 200),
    ("POST", "/checked", {"json": {}}, 422),
    ("GET", "/deps/1", {"headers": {"X-Token": "t", "x-owner": "o"}}, 200),
    ("GET", "/deps/1", {"headers": {"x-owner": "o"}}, 422),
    ("GET", "/levels", {"params": {"z": 1, "b": 2, "c": 3, "d": 4}}, 200),
    ("GET", "/levels", {"params": {"z": 1}}, 422),
    ("GET", "/joined", {"headers": {"X-Token": "t"}}, 200),
    ("GET", "/joined", {}, 422),
    ("GET", "/dynamic", {"params": {"name": "a"}}, 200),
    ("GET", "/dynamic", {}, 422),
    ("GET", "/search", {}, 200),
    ("GET", "/search", {"params": {"page": 0}}, 422),
    ("GET", "/admin/t1/stats", {"headers": {"X-API-Key": "k", "x-tenant": "t"}}, 200),
    ("GET", "/admin/t1/stats", {"headers": {"X-API-Key": "k"}}, 422),
    ("GET", "/admin/t1/raw/3", {"headers": {"X-API-Key": "k", "x-tenant": "t"}}, 200),
    ("GET", "/admin/t1/raw/3", {"headers": {"X-API-Key": "k"}}, 422),
    ("GET", "/lambda", {"params": {"a": 1}}, 200),
    ("GET", "/lambda", {}, 422),
    ("POST", "/serialized", {"json": {"name": "a"}}, 200),
    ("GET", "/hidden", {"params": {"q": 1}}, 200),
    ("GET", "/hidden", {}, 422),
    ("GET", "/annotated-list", {}, 200),
    ("GET", "/me", {"headers": {"Authorization": "Bearer t"}}, 200),
    ("GET", "/ambiguous", {}, 200),
    ("GET", "/caught/x", {}, 422),
    ("GET", "/escape", {}, 200),
    ("GET", "/external", {}, 200),
    ("GET", "/field", {}, 200),
    ("GET", "/files/a/1", {}, 200),
    ("GET", "/guarded/1", {}, 200),
    ("GET", "/html", {}, 200),
    ("GET", "/keys", {"headers": {"X-API-Key": "k", "Authorization": "Bearer t"}}, 401),
    ("GET", "/local", {}, 200),
    ("GET", "/nomodel", {}, 200),
    ("GET", "/none", {}, 200),
    ("GET", "/oidc", {"headers": {"Authorization": "Bearer t"}}, 200),
    ("GET", "/optional", {}, 200),
    ("GET", "/plain", {}, 200),
    ("GET", "/raw", {"headers": {"User-Agent": "x"}}, 200),
    ("GET", "/returned", {}, 200),
    ("GET", "/returned-direct/1", {}, 200),
    ("GET", "/scoped", {"headers": {"Authorization": "Bearer t"}}, 200),
    ("GET", "/spread", {}, 500),
    ("GET", "/try/1", {}, 200),
    ("POST", "/webhook", {"json": {"a": 1}}, 200),
    ("POST", "/serialized", {"json": {}}, 422),
    # The review's classes (app/routers/review.py).
    ("GET", "/url", {"params": {"a": "1"}}, 200),
    ("GET", "/state", {}, 200),
    ("GET", "/indirect", {}, 202),
    ("GET", "/indirect2", {}, 202),
    ("GET", "/proven-plain", {}, 200),
    ("GET", "/exclnone", {}, 200),
    ("GET", "/exclude", {}, 200),
    ("GET", "/byalias", {}, 200),
    ("GET", "/unset", {}, 200),
    ("POST", "/formmodel", {"data": {"username": "a", "age": "3"}}, 200),
    ("POST", "/formmodel", {"data": {"data": "x"}}, 422),
    ("POST", "/popname", {"json": {"user_name": "a"}}, 200),
    ("POST", "/popname", {"json": {}}, 422),
    ("GET", "/redir", {"follow_redirects": False}, 307),
    ("GET", "/gen", {}, 200),
    ("GET", "/stream-items", {}, 200),
    ("GET", "/bv", {"params": {"q": "abc"}}, 200),
    ("GET", "/av", {"params": {"q": "-3"}}, 200),
    ("GET", "/av", {"params": {"q": "x"}}, 422),
    ("POST", "/fv", {"json": {"tags": "a,b"}}, 200),
    ("POST", "/fv", {"json": {}}, 422),
    ("POST", "/fv-after", {"json": {"tags": ["a"]}}, 200),
    ("POST", "/fv-after", {"json": {}}, 422),
    ("GET", "/enum-missing", {"params": {"c": "RED"}}, 200),
    ("GET", "/opt422", {}, 200),
    ("GET", "/after-yield", {}, 200),
    ("GET", "/cookie-oauth", {}, 401),
    ("GET", "/cookie-oauth", {"cookies": {"access_token": "t"}}, 200),
    ("POST", "/files-list", {"files": [("files", ("a.txt", b"x")), ("files", ("b.txt", b"y"))]}, 200),
    ("POST", "/files-list", {"json": []}, 422),
    ("GET", "/reassign", {}, 200),
    ("GET", "/cookie-set", {}, 201),
    ("GET", "/custom", {"params": {"q": 1}}, 299),
    ("GET", "/computed", {}, 200),
]

# The handlers fixture: every raised status and the 422 are rewritten.
HANDLER_CASES = [
    ("GET", "/missing", {"params": {"q": 1}}, 200),
    ("GET", "/missing", {}, 400),
    ("GET", "/teapot", {"params": {"q": 1}}, 200),
    ("GET", "/teapot", {"params": {"q": "x"}}, 400),
    ("GET", "/annotated", {}, 200),
]

# The unpack fixture (T029 FU-1): defaults inherited from objects
# constructed or included with keyword unpacking or default_response_class
# (CreatedJSON sends 201; the unpacked sub-app's handler rewrites
# HTTPException to 418), and the routes those objects do not serve.
UNPACK_CASES = [
    ("GET", "/root", {}, 200),
    ("GET", "/plain/1", {}, 200),
    ("GET", "/plain/-1", {}, 404),
    ("GET", "/plain/x", {}, 422),
    ("GET", "/shared", {}, 200),
    ("GET", "/sub/shared", {}, 201),
    ("GET", "/styled/page", {}, 201),
    ("GET", "/unpacked/made", {}, 201),
    ("GET", "/unpacked/nested", {}, 201),
    ("GET", "/sub/item/1", {}, 201),
    ("GET", "/sub/item/-1", {}, 418),
    ("GET", "/sub/item/x", {}, 422),
]

# The assign fixture: default_response_class = RedirectResponse assigned
# on the app's router and on a router (307, redirects not followed).
ASSIGN_CASES = [
    ("GET", "/go", {"follow_redirects": False}, 307),
    ("GET", "/hop", {"follow_redirects": False}, 307),
    ("GET", "/target", {}, 200),
]

SUCCESS_RULES = ("SH-F24", "SH-F25", "SH-F26", "SH-F27")

# Handlers that never return normally (endless recursion, an undefined
# name, an unserializable value, HTTP bearer and basic both wanting the one
# Authorization header): their declared success status is FastAPI's, not
# reachable here. Row SH-F24 states the status FastAPI sends when the
# handler returns, never that it returns.
NEVER_RETURN = {("GET", "/cycle"), ("GET", "/unresolved"), ("GET", "/spread"), ("GET", "/keys")}

SAMPLES = {"integer": "1", "number": "1.5", "boolean": "true", "string": "ab"}


def sample(schema):
    if schema is None:
        return "ab"
    if schema.get("type") == "enum":
        return str(schema["enum"][0])
    if schema.get("type") == "array":
        return [sample(schema.get("items"))]
    return SAMPLES.get(schema.get("type"), None)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", required=True)
    parser.add_argument("--app", required=True)
    parser.add_argument("--ndjson", required=True)
    parser.add_argument("--fixture", default="main", choices=["main", "handlers", "unpack", "assign"])
    args = parser.parse_args()
    cases = {"main": CASES, "handlers": HANDLER_CASES, "unpack": UNPACK_CASES, "assign": ASSIGN_CASES}[args.fixture]
    sys.path.insert(0, args.root)
    import importlib

    from fastapi.testclient import TestClient

    module, attribute = args.app.split(":")
    app = getattr(importlib.import_module(module), attribute)
    client = TestClient(app, raise_server_exceptions=False)
    counts, problems = {}, []

    def count(key):
        counts[key] = counts.get(key, 0) + 1

    routes = []
    with open(args.ndjson) as f:
        for line in f:
            o = json.loads(line)
            if o.get("kind") == "route" and o["attributes"].get("shape") and o["attributes"].get("paths"):
                routes.append(o)
    driven = {}
    bodies = {}
    for method, path, kwargs, want in cases:
        resp = client.request(method, path, **kwargs)
        got = resp.status_code
        if got != want:
            problems.append(f"case {method} {path} {kwargs}: status {got}, expected {want}")
        driven.setdefault((method, path), set()).add(got)
        if resp.headers.get("content-type", "").split(";")[0].strip() == "application/json" and resp.content:
            bodies.setdefault((method, path, got), resp.json())
    templates = {}
    for r in routes:
        for entry in r["attributes"]["paths"]:
            templates[(r["attributes"]["method"], entry["template"])] = r
    for (method, template), r in sorted(templates.items()):
        sh = r["attributes"]["shape"]
        # Which concrete paths of the cases belong to this template.
        reached = set()
        for (m, p), statuses in driven.items():
            if m == method and matches(template, p):
                reached |= statuses
        models = {m["name"]: m["schema"] for m in sh.get("models", [])}
        check_unpacked(method, template, sh, reached, count, problems)
        for item in sh["responses"]["items"]:
            if item["provenance"]["kind"] != "fact":
                lowered = ("SH-F29", "SH-F30")
                reason = sh["responses"]["reason"]
                # Lowered by a registered handler or a route class (not
                # only by an unpacked FastAPI, which may pass no handler:
                # check_unpacked counts those).
                if item["provenance"]["rule"] in lowered and ("an exception handler for" in reason or "route_class" in reason):
                    if int(item["status"]) in reached:
                        problems.append(f"{method} {template}: status {item['status']} lowered by a handler or route class is sent")
                    else:
                        count("statuses lowered by handlers or route classes never sent")
                continue
            if (method, template) in NEVER_RETURN and item["provenance"]["rule"] in ("SH-F24", "SH-F25", "SH-F26", "SH-F27"):
                count("success statuses of handlers that never return")
                continue
            if item["status"] == "422" and 422 not in reached and drive_422(client, method, template, sh):
                reached.add(422)
                count("fact 422 reached by a generated invalid request")
            if int(item["status"]) in reached:
                count("fact statuses reached")
            else:
                problems.append(f"{method} {template}: fact status {item['status']} ({item['provenance']['rule']}) is not reached by any request")
            if item["provenance"]["rule"] in ("SH-F24", "SH-F25", "SH-F26", "SH-F27") and item.get("schema") and item["schema"]["type"] not in ("unknown", "null"):
                body = None
                for (m, p, st), b in bodies.items():
                    if m == method and st == int(item["status"]) and matches(template, p):
                        body = b
                if body is None:
                    problems.append(f"{method} {template}: no driven success body to check against the claimed schema")
                else:
                    why = validate(body, item["schema"], models)
                    if why:
                        problems.append(f"{method} {template}: success body {body!r} disagrees with the claimed schema: {why}")
                    else:
                        count("success bodies valid against the claimed schema")
        check_body(client, method, template, sh, models, count, problems)
        if any(i["provenance"]["kind"] == "intended" and i["provenance"]["rule"] in ("SH-F24", "SH-F25", "SH-F26", "SH-F27") for i in sh["responses"]["items"]):
            declared = [int(i["status"]) for i in sh["responses"]["items"] if i["provenance"]["rule"] in ("SH-F24", "SH-F25", "SH-F26", "SH-F27")]
            if reached - set(declared) - {422}:
                count("lowered success responses shown overridden")
        check_params(client, method, template, sh, count, problems)
    for k in sorted(counts):
        print(f"{k}: {counts[k]}")
    print(f"disagreements: {len(problems)}")
    for p in problems:
        print("  " + p)
    sys.exit(1 if problems else 0)


def check_unpacked(method, template, sh, reached, count, problems):
    """Rows SH-F24 and SH-F37 for inherited defaults (T029 FU-1): a route
    that may inherit a default_response_class claims no success content
    type (and, without status_code=, no success item: a fact item must be
    reached like any other);
    where the request reached it through an object that sets one, the
    status shows the default applied. Raised statuses lowered by an
    unpacked FastAPI are either rewritten (that app serves the route) or
    sent as raised (another app does)."""
    reason = sh["responses"].get("reason", "")
    items = sh["responses"]["items"]
    if "default_response_class" in reason:
        if any(i["provenance"]["rule"] in SUCCESS_RULES and i.get("content_type") for i in items):
            problems.append(f"{method} {template}: a success content type is claimed although a default_response_class may apply")
        elif reached - {200, 404, 418, 422}:
            count("withheld success statuses shown changed by the inherited default")
        else:
            count("withheld success statuses not changed on this path (another including object)")
    if "may pass exception_handlers" in reason:
        for i in items:
            if i["provenance"]["rule"] not in ("SH-F29", "SH-F30"):
                continue
            if int(i["status"]) in reached:
                count("statuses lowered by an unpacked FastAPI sent unchanged (another app, or no such handler passed)")
            else:
                count("statuses lowered by an unpacked FastAPI shown rewritten")


def validate(value, schema, models, depth=0):
    """Returns why value does not satisfy the claimed schema, or ""."""
    if depth > 20 or schema is None:
        return ""
    t = schema.get("type")
    if value is None:
        return "" if schema.get("nullable") or t in ("null", "unknown") else "null for a non-nullable schema"
    if t == "unknown":
        return ""
    if t == "ref":
        target = models.get(schema["ref"])
        if target is None or target.get("type") != "object":
            return ""
        return validate(value, target, models, depth + 1) or names_only(value, target)
    if t == "object":
        if not isinstance(value, dict):
            return f"not an object: {value!r}"
        for f in schema.get("fields", []):
            if f["name"] not in value:
                if f["presence"] == "required":
                    return f"required field {f['name']} missing"
                continue
            why = validate(value[f["name"]], f["schema"], models, depth + 1)
            if why:
                return f"field {f['name']}: {why}"
        return ""
    if t == "array":
        if not isinstance(value, list):
            return f"not an array: {value!r}"
        for v in value:
            why = validate(v, schema.get("items"), models, depth + 1)
            if why:
                return why
        return ""
    if t == "enum":
        return "" if value in schema["enum"] else f"{value!r} not in {schema['enum']}"
    ok = {"string": isinstance(value, str), "integer": isinstance(value, int) and not isinstance(value, bool),
          "number": isinstance(value, (int, float)) and not isinstance(value, bool), "boolean": isinstance(value, bool),
          "null": False}.get(t, True)
    return "" if ok else f"{value!r} is not {t}"


def names_only(value, model):
    """A response model's output holds only its fields."""
    names = {f["name"] for f in model.get("fields", [])}
    extra = sorted(set(value) - names) if isinstance(value, dict) else []
    return f"fields {extra} outside the model" if extra else ""


def build(schema, models, depth=0):
    """A value with exactly the required fields; None when not built."""
    if schema is None or depth > 10:
        return None
    t = schema.get("type")
    if t == "ref":
        return build(models.get(schema["ref"]), models, depth + 1)
    if t == "object":
        out = {}
        for f in schema.get("fields", []):
            if f["presence"] == "required":
                v = build(f["schema"], models, depth + 1)
                if v is None and not f["schema"].get("nullable"):
                    return None
                out[f["name"]] = v
        return out
    if t == "array":
        item = build(schema.get("items"), models, depth + 1)
        return None if item is None else [item]
    if t == "enum":
        return schema["enum"][0]
    return {"string": "ab", "integer": 1, "number": 1.5, "boolean": True}.get(t)


def required_params(sh, template):
    """The path and required query/header/cookie values, or None."""
    path = template
    for it in sh["path"]["items"]:
        path = path.replace("{" + it["name"] + "}", str(sample(it.get("schema")) or "ab"))
    out = {"params": {}, "headers": {}, "cookies": {}}
    for section, key in (("query", "params"), ("header", "headers"), ("cookie", "cookies")):
        for it in sh[section]["items"]:
            if it["presence"] == "required":
                v = sample(it.get("schema"))
                if v is None:
                    return None, None
                out[key][it["name"]] = v
    return path, out


def check_body(client, method, template, sh, models, count, problems):
    """Claimed required body fields: exactly them succeed, each missing is 422."""
    if sh["security"]["items"]:
        return
    facts = [c for c in sh["body"]["items"] if c["provenance"]["kind"] == "fact" and c.get("schema")]
    if len(facts) != 1:
        return
    content = facts[0]
    schema = content["schema"]
    target = models.get(schema["ref"]) if schema.get("type") == "ref" else schema
    if not target or target.get("type") != "object":
        return
    value = build(schema, models)
    path, base = required_params(sh, template)
    if value is None or path is None or "{" in path:
        count("bodies not checked (a value not built)")
        return

    def send(body):
        kw = dict(base)
        ct = content["content_type"]
        if ct == "application/json":
            kw["json"] = body
        else:
            # File fields (the fixture names them file and files) are sent
            # as uploads, one part per list element.
            files = []
            for k, v in body.items():
                if is_file(target, k):
                    files += [(k, ("f.txt", b"x"))] * (len(v) if isinstance(v, list) else 1)
            data = {k: (json.dumps(v) if isinstance(v, (dict, list)) else v) for k, v in body.items() if not is_file(target, k)}
            kw["data"] = data
            if files or ct == "multipart/form-data":
                kw["files"] = files or [("_", ("f.txt", b"x"))]
        return client.request(method, path, **kw)

    resp = send(value)
    if resp.status_code == 422:
        problems.append(f"{method} {template}: a body with exactly the claimed required fields {value!r} is refused: {resp.text[:200]}")
        return
    count("bodies with the claimed required fields accepted")
    for name in value:
        rest = {k: v for k, v in value.items() if k != name}
        resp = send(rest)
        locs = [e.get("loc", []) for e in resp.json().get("detail", [])] if resp.status_code == 422 else []
        if any(name in loc for loc in locs):
            count("claimed required body fields refused when missing")
        else:
            problems.append(f"{method} {template}: body field {name} claimed required, missing gives {resp.status_code}")


def is_file(schema, name):
    return name in ("file", "files")


def drive_422(client, method, template, sh):
    """Tries invalid requests built from the claims; True when one is 422."""
    path, base = required_params(sh, template)
    if path is None:
        return False
    attempts = []
    for section, key in (("query", "params"), ("header", "headers"), ("cookie", "cookies")):
        for it in sh[section]["items"]:
            if it["access"] != "declared":
                continue
            kw = {k: dict(v) for k, v in base.items()}
            if it["presence"] == "required":
                kw[key].pop(it["name"], None)
                attempts.append((path, kw))
            if it.get("schema") and it["schema"].get("type") in ("integer", "number", "boolean", "enum"):
                kw = {k: dict(v) for k, v in base.items()}
                kw[key][it["name"]] = "zz"
                attempts.append((path, kw))
    for it in sh["path"]["items"]:
        if it.get("schema") and it["schema"].get("type") in ("integer", "number", "boolean", "enum"):
            attempts.append((template.replace("{" + it["name"] + "}", "zz"), base))
    if sh["body"]["items"]:
        kw = dict(base)
        kw["content"] = b"{not json"
        kw["headers"] = dict(base["headers"], **{"content-type": "application/json"})
        attempts.append((path, kw))
    for p, kw in attempts:
        p = templateish(p, sh)
        if "{" in p:
            continue
        if client.request(method, p, **kw).status_code == 422:
            return True
    return False


def templateish(p, sh):
    for it in sh["path"]["items"]:
        p = p.replace("{" + it["name"] + "}", str(sample(it.get("schema")) or "ab"))
    return p


def matches(template, path):
    t, p = template.strip("/").split("/"), path.strip("/").split("/")
    return len(t) == len(p) and all(a == b or a.startswith("{") for a, b in zip(t, p))


def check_params(client, method, template, sh, count, problems):
    """Leave each declared parameter out of an otherwise complete request."""
    if sh["body"]["items"] or sh["security"]["items"]:
        return
    declared = {}
    for section in ("query", "header", "cookie"):
        for it in sh[section]["items"]:
            if it["access"] == "declared" and it["provenance"]["kind"] == "fact":
                value = sample(it["schema"])
                if value is None:
                    return
                declared[(section, it["name"])] = (it["presence"], value)
    path = template
    for it in sh["path"]["items"]:
        if it["access"] == "declared":
            path = path.replace("{" + it["name"] + "}", sample(it["schema"]) or "ab")
    if "{" in path:
        return

    def request(skip):
        params, headers, cookies = {}, {}, {}
        for (section, name), (presence, value) in declared.items():
            if (section, name) == skip or presence != "required" and skip == "optional":
                continue
            {"query": params, "header": headers, "cookie": cookies}[section][name] = value
        return client.request(method, path, params=params, headers=headers, cookies=cookies)

    for (section, name), (presence, _) in declared.items():
        if presence != "required":
            continue
        resp = request((section, name))
        locs = [e.get("loc", []) for e in resp.json().get("detail", [])] if resp.status_code == 422 else []
        rewritten = "an exception handler for" in sh["responses"]["reason"] and 400 <= resp.status_code < 500
        if any(name in loc for loc in locs):
            count("required parameters refused when missing")
        elif rewritten:
            count("required parameters refused when missing (422 rewritten by a handler)")
        else:
            problems.append(f"{method} {template}: {section} {name} claimed required, missing gives {resp.status_code}")
    resp = request("optional")
    locs = [e.get("loc", []) for e in resp.json().get("detail", [])] if resp.status_code == 422 else []
    for (section, name), (presence, _) in declared.items():
        if presence == "optional":
            if any(name in loc for loc in locs):
                problems.append(f"{method} {template}: {section} {name} claimed optional, refused when missing")
            else:
                count("optional parameters accepted when missing")


if __name__ == "__main__":
    main()
