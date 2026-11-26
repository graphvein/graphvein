"""Oracle for the Axum route shape claims (T029).

Starts a fixture server (axum 0.8.8 or 0.3.4), drives every route branch
over HTTP, and compares Graphvein's claimed shapes (the fixture golden)
with what the real framework does: claimed statuses and content types are
observed; claimed required/optional fields and parameters are refused /
accepted when left out; claimed types refuse a wrong-kind value; claimed
enums accept every value and refuse another; claimed closed objects refuse
an unknown field; responses and serde samples conform to claimed schemas.
"""
import copy
import http.client
import json
import subprocess
import sys
import time

binary, golden_path, version, port = sys.argv[1], sys.argv[2], sys.argv[3], int(sys.argv[4])
golden = json.load(open(golden_path))["shapes"]
proc = subprocess.Popen([binary, str(port)], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
samples = {}
if version == "0.8":
    samples = json.loads(proc.stdout.readline())
time.sleep(1.5)

results = {"agree": 0, "disagree": 0}
log = []


def check(ok, what):
    results["agree" if ok else "disagree"] += 1
    log.append(("OK  " if ok else "FAIL") + " " + what)


def request(method, path, body=None, headers=None, ctype=None):
    c = http.client.HTTPConnection("127.0.0.1", port, timeout=10)
    h = dict(headers or {})
    data = None
    if body is not None:
        if isinstance(body, (dict, list)):
            data = json.dumps(body).encode()
            h.setdefault("content-type", ctype or "application/json")
        else:
            data = body.encode()
            if ctype:
                h["content-type"] = ctype
    c.request(method, path, body=data, headers=h)
    r = c.getresponse()
    out = (r.status, r.getheader("content-type"), r.read())
    c.close()
    return out


def shape(route):
    for k, v in golden.items():
        if k.endswith("#" + route):
            return v
    raise KeyError(route)


def conforms(value, s, path=""):
    """Whether a serialized value fits a claimed schema; returns a reason."""
    if value is None:
        if s.get("nullable") or s["type"] in ("null", "unknown"):
            return None
        return f"{path}: null not claimed"
    t = s["type"]
    if t == "unknown":
        return None
    if t == "object":
        if not isinstance(value, dict):
            return f"{path}: not an object"
        names = {f["name"]: f for f in s.get("fields", [])}
        for k, v in value.items():
            if k not in names:
                if s["additional"] == "forbidden":
                    return f"{path}.{k}: key not claimed"
                continue
            r = conforms(v, names[k]["schema"], path + "." + k)
            if r:
                return r
        for n, f in names.items():
            if f["presence"] == "required" and n not in value:
                return f"{path}.{n}: required key missing"
        return None
    if t == "array":
        if not isinstance(value, list):
            return f"{path}: not an array"
        for i, v in enumerate(value):
            r = conforms(v, s["items"], f"{path}[{i}]")
            if r:
                return r
        return None
    if t == "enum":
        return None if value in s["enum"] else f"{path}: {value!r} not in enum"
    kinds = {"string": str, "boolean": bool, "integer": int, "number": (int, float)}
    if t in kinds:
        if isinstance(value, bool) and t != "boolean":
            return f"{path}: bool"
        return None if isinstance(value, kinds[t]) else f"{path}: not {t}"
    if t == "null":
        return None if value is None else f"{path}: not null"
    return f"{path}: type {t}"


def sample(s):
    """A value that fits a claimed (request) schema."""
    t = s["type"]
    if t == "object":
        return {f["name"]: sample(f["schema"]) for f in s.get("fields", []) if f["schema"]["type"] != "unknown"}
    return {"string": "s", "integer": 1, "number": 1.5, "boolean": True, "array": [], "enum": None, "null": None}[t] if t != "enum" else s["enum"][0]


def wrong(s):
    return {"string": 7, "integer": "x", "number": "x", "boolean": "x", "array": "x", "object": "x", "enum": "zz-not-a-value"}.get(s["type"])


# ---- responses: every claimed (status, content type) is reachable ----
observed = {}


def probe(route, method, path, **kw):
    st, ct, body = request(method, path, **kw)
    observed.setdefault(route, []).append((st, ct, body))
    return st, ct, body


if version == "0.8":
    tok = {"authorization": "Bearer tok"}
    newitem = sample(shape("POST /items")["body"]["items"][0]["schema"])
    calls = [
        ("DELETE /items/{id}", "DELETE", "/v1/items/1", {"headers": {"x-api-key": "k"}}),
        ("GET /deep", "GET", "/v1/deep", {"headers": {"x-l1": "1", "x-l2": "1", "x-l3": "1", "x-l4": "1"}}),
        ("GET /deep", "GET", "/v1/deep", {}),
        ("GET /loop", "GET", "/v1/loop", {"headers": {"x-ping": "1"}}),
        ("GET /loop", "GET", "/v1/loop", {}),
        ("GET /files/{*rest}", "GET", "/v1/files/a/b", {}),
        ("GET /form", "GET", "/v1/form?user=a", {}),
        ("GET /health", "GET", "/v1/health", {}),
        ("GET /items", "GET", "/v1/items?q=x", {}),
        ("GET /items", "GET", "/v1/items?q=", {}),
        ("GET /items/{id}", "GET", "/v1/items/3", {}),
        ("GET /pair/{a}/{b}", "GET", "/v1/pair/a/1", {}),
        ("GET /redirect", "GET", "/v1/redirect", {}),
        ("GET /render", "GET", "/v1/render", {}),
        ("GET /search", "GET", "/v1/search?term=a", {}),
        ("GET /search", "GET", "/v1/search?term=a", {"headers": {"x-dry-run": "1"}}),
        ("GET /whoami", "GET", "/v1/whoami", {"headers": {"user-agent": "ua"}}),
        ("POST /echo", "POST", "/v1/echo", {"body": "hello", "ctype": "text/plain"}),
        ("POST /events", "POST", "/v1/events", {"body": []}),
        ("POST /events", "POST", "/v1/events", {"body": [{"x": 1, "y": 2}]}),
        ("POST /form", "POST", "/v1/form", {"body": "user=a&remember=true", "ctype": "application/x-www-form-urlencoded"}),
        ("POST /items", "POST", "/v1/items", {"body": newitem, "headers": tok}),
        ("POST /items", "POST", "/v1/items", {"body": newitem, "headers": {"authorization": "Bearer revoked"}}),
        ("POST /maybe", "POST", "/v1/maybe", {"body": newitem}),
        ("POST /maybe", "POST", "/v1/maybe", {}),
        ("POST /raw", "POST", "/v1/raw", {"headers": {"x-debug": "1"}}),
        ("POST /raw", "POST", "/v1/raw", {}),
        ("POST /wrapped", "POST", "/v1/wrapped", {"body": {"data": {"events": [{"x": 1, "y": 2}, "k"]}, "count": 1}}),
        ("PUT /items/{id}/tags/{tag}", "PUT", "/v1/items/1/tags/x", {}),
        # Forms of the independent review (src/review.rs).
        ("GET /r/guard", "GET", "/v1/r/guard?auth=1", {}),
        ("GET /r/guard", "GET", "/v1/r/guard?m=x", {}),
        ("GET /r/guard", "GET", "/v1/r/guard", {}),
        ("GET /r/if", "GET", "/v1/r/if?gone=1", {}),
        ("GET /r/if", "GET", "/v1/r/if?x=1", {}),
        ("GET /r/if", "GET", "/v1/r/if", {}),
        ("GET /r/arms", "GET", "/v1/r/arms?name=", {}),
        ("GET /r/arms", "GET", "/v1/r/arms?name=a", {}),
        ("GET /r/arms", "GET", "/v1/r/arms", {}),
        ("GET /r/only-err", "GET", "/v1/r/only-err", {}),
        ("POST /r/serde-as", "POST", "/v1/r/serde-as", {"body": {"id": "5"}}),
        ("GET /r/cfg-named", "GET", "/v1/r/cfg-named", {}),
        ("POST /r/camel", "POST", "/v1/r/camel", {"body": {"foo": 1, "barBaz": 2}}),
        ("GET /r/lower", "GET", "/v1/r/lower?userId=3", {}),
        ("GET /r/q-forms", "GET", "/v1/r/q-forms?limit=2&sort=asc", {}),
        ("GET /r/problem", "GET", "/v1/r/problem", {}),
        ("GET /r/traced", "GET", "/v1/r/traced", {}),
        ("GET /r/problem-dyn", "GET", "/v1/r/problem-dyn?ct=text/x-zz", {}),
        ("GET /r/two-json", "GET", "/v1/r/two-json?a=1", {}),
        ("GET /r/two-json", "GET", "/v1/r/two-json", {}),
        ("POST /r/shadow", "POST", "/v1/r/shadow", {"body": "bob", "ctype": "text/plain"}),
        ("POST /r/shadow", "POST", "/v1/r/shadow", {"body": "", "ctype": "text/plain"}),
        ("POST /r/shadow-helper", "POST", "/v1/r/shadow-helper", {"body": {"name": "bob"}}),
        ("GET /r/metric", "GET", "/v1/r/metric", {}),
        ("ANY /r/any-form", "GET", "/v1/r/any-form?user=bob", {}),
        ("ANY /r/any-form", "POST", "/v1/r/any-form", {"body": "user=al", "ctype": "application/x-www-form-urlencoded"}),
        ("HEAD /r/form-head", "HEAD", "/v1/r/form-head", {"body": "user=a", "ctype": "application/x-www-form-urlencoded"}),
        ("GET /r/closure-shadow", "GET", "/v1/r/closure-shadow", {}),
        ("GET /r/via-extract", "GET", "/v1/r/via-extract?term=x", {}),
        ("GET /r/deep-query", "GET", "/v1/r/deep-query?t=1", {}),
        ("GET /r/via-macro", "GET", "/v1/r/via-macro?term=x", {}),
        ("POST /r/boxed", "POST", "/v1/r/boxed", {"body": {"n": 1}}),
    ]
else:
    auth = {"authorization": "Token abc"}
    calls = [
        ("GET /api/users/:id", "GET", "/api/users/1", {}),
        ("GET /api/users/:id", "GET", "/api/users/-1", {}),
        ("PUT /api/users/:id", "PUT", "/api/users/1", {"body": {"user": {"bio": "b"}}, "headers": auth}),
        ("POST /api/users", "POST", "/api/users", {"body": {"user": {"username": "a", "email": "e"}}}),
        ("POST /api/users", "POST", "/api/users", {"body": {"user": {"username": "a", "email": ""}}}),
        ("GET /api/feed", "GET", "/api/feed?limit=2", {"headers": auth}),
        ("GET /api/ping", "GET", "/api/ping", {}),
        ("GET /api/page", "GET", "/api/page", {}),
        ("GET /api/hello", "GET", "/api/hello", {}),
        ("POST /api/created", "POST", "/api/created", {}),
        ("POST /api/headers-first", "POST", "/api/headers-first", {"body": {"user": {"username": "a", "email": "e"}}, "headers": {"x-a": "1"}}),
        ("HEAD /api/form-head", "HEAD", "/api/form-head", {"body": "user=a", "ctype": "application/x-www-form-urlencoded"}),
        ("HEAD /api/form-head", "HEAD", "/api/form-head", {"body": "user=", "ctype": "application/x-www-form-urlencoded"}),
    ]
for route, method, path, kw in calls:
    probe(route, method, path, **kw)

for route, sh in golden.items():
    if isinstance(sh, str):
        continue
    name = route.split("#", 1)[1]
    for it in sh["responses"]["items"]:
        seen = [o for o in observed.get(name, []) if str(o[0]) == it["status"]]
        check(bool(seen), f"{name}: status {it['status']} observed")
        for st, ct, body in seen[:1]:
            if it["content_type"] is not None:
                check(ct == it["content_type"], f"{name} {st}: content type {ct!r} == claimed {it['content_type']!r}")
            if it["schema"] is not None:
                if it["schema"]["type"] == "null":
                    check(body == b"", f"{name} {st}: empty body")
                elif it["schema"]["type"] != "unknown":
                    ct = it["content_type"] or ""
                    as_json = ct.endswith("json") or (not ct and it["schema"]["type"] not in ("string",))
                    value = json.loads(body) if as_json else body.decode()
                    r = conforms(value, it["schema"])
                    check(r is None, f"{name} {st}: body conforms ({r})")

# ---- serde samples (0.8): serialized values conform to the claimed schemas ----
if version == "0.8":
    resp = lambda route, status: next(i["schema"] for i in shape(route)["responses"]["items"] if i["status"] == status)
    for key, route, status in [("Item/note_none", "GET /items/{id}", "200"), ("Item/note_some", "GET /items/{id}", "200"),
                               ("Health", "GET /health", "200"), ("TagResult", "PUT /items/{id}/tags/{tag}", "200"),
                               ("Envelope<Kind>", "POST /wrapped", "200"), ("Envelope<Vec<Item>>", "GET /items", "200")]:
        r = conforms(samples[key], resp(route, status))
        check(r is None, f"serde sample {key} conforms to {route} {status} ({r})")
    kinds = resp("GET /items/{id}", "200")
    enum = next(f for f in kinds["fields"] if f["name"] == "kind")["schema"]["enum"]
    check(sorted(samples["Kind"]) == sorted(enum), f"Kind serializes to {samples['Kind']} == claimed {enum}")
    item = resp("GET /items/{id}", "200")
    names = sorted(f["name"] for f in item["fields"])
    check(sorted(set(samples["Item/note_some"])) == names, f"Item keys {sorted(samples['Item/note_some'])} == claimed {names}")

# ---- request claims: body fields, query and path parameters, headers ----


def body_probes(route, method, path, accept, headers, wrap=None):
    """Leave out each claimed field, give it a wrong kind, null, unknown key."""
    sh = shape(route)
    schema = sh["body"]["items"][0]["schema"]
    ct = sh["body"]["items"][0]["content_type"]
    form = ct == "application/x-www-form-urlencoded"

    def send(obj):
        if form:
            from urllib.parse import urlencode
            return request(method, path, body=urlencode({k: str(v).lower() if isinstance(v, bool) else v for k, v in obj.items()}), headers=headers, ctype=ct)[0]
        return request(method, path, body=obj, headers=headers)[0]

    def walk(schema, base, setter, where):
        for f in schema.get("fields", []):
            if f["schema"]["type"] == "unknown":
                continue
            obj = setter(base, lambda o: o.pop(f["name"], None))
            st = send(obj)
            if f["presence"] == "required":
                check(400 <= st < 500, f"{route} {where}{f['name']} left out: refused ({st})")
            elif f["presence"] == "optional":
                check(st == accept, f"{route} {where}{f['name']} left out: accepted ({st})")
            w = wrong(f["schema"])
            if w is not None and not form:
                st = send(setter(base, lambda o: o.__setitem__(f["name"], w)))
                check(400 <= st < 500, f"{route} {where}{f['name']} wrong kind: refused ({st})")
            if not form:
                st = send(setter(base, lambda o: o.__setitem__(f["name"], None)))
                if f["schema"].get("nullable"):
                    check(st == accept, f"{route} {where}{f['name']} null: accepted ({st})")
                elif f["schema"]["type"] != "object" or f["schema"].get("fields"):
                    check(400 <= st < 500, f"{route} {where}{f['name']} null: refused ({st})")
            if f["schema"]["type"] == "enum":
                for v in f["schema"]["enum"]:
                    st = send(setter(base, lambda o: o.__setitem__(f["name"], v)))
                    check(st == accept, f"{route} {where}{f['name']}={v}: accepted ({st})")
            if f["schema"]["type"] == "object" and f["schema"].get("fields") and not form:
                inner = f["schema"]
                walk(inner, base, lambda b, op, n=f["name"]: setter(b, lambda o: (o.__setitem__(n, copy.deepcopy(o[n]) if isinstance(o.get(n), dict) else sample(inner)), op(o[n]))), where + f["name"] + ".")
        if schema.get("additional") in ("forbidden", "allowed") and not form:
            st = send(setter(base, lambda o: o.__setitem__("zzUnknownKey", 1)))
            ok = (400 <= st < 500) if schema["additional"] == "forbidden" else st == accept
            check(ok, f"{route} {where}unknown key: {'refused' if schema['additional'] == 'forbidden' else 'accepted'} ({st})")

    base = sample(schema)
    check(send(base) == accept, f"{route}: sample body accepted")

    def top(b, op):
        o = copy.deepcopy(b)
        op(o)
        return o
    walk(schema, base, top, "")
    if not form:
        st = request(method, path, body=json.dumps(base), headers=headers)[0]
        check(st == 415 or (version == "0.3" and 400 <= st < 500), f"{route}: body without content type refused ({st})")


def query_probes(route, path, accept, headers=None, method="GET"):
    from urllib.parse import urlencode
    sh = shape(route)
    items = [i for i in sh["query"]["items"] if i["provenance"]["kind"] == "fact"]
    # An item whose schema is unknown is left out of the sample (only its
    # presence is claimed: an optional one left out is accepted).
    known = [i for i in items if not i["schema"] or i["schema"]["type"] != "unknown"]
    base = {i["name"]: sample(i["schema"]) if i["schema"] else "x" for i in known}
    base = {k: (str(v).lower() if isinstance(v, bool) else v) for k, v in base.items()}
    if "q" in base:
        base["q"] = "x"
    st = request(method, path + "?" + urlencode(base), headers=headers)[0]
    check(st == accept, f"{route}: sample query accepted ({st})")
    for i in items:
        if i not in known:
            check(i["presence"] == "optional", f"{route} query {i['name']} (schema unknown) claimed optional and left out of the accepted sample")
            continue
        q = dict(base)
        q.pop(i["name"])
        st = request(method, path + "?" + urlencode(q), headers=headers)[0]
        if i["presence"] == "required":
            check(st == 400, f"{route} query {i['name']} left out: refused ({st})")
        else:
            check(st == accept, f"{route} query {i['name']} left out: accepted ({st})")
        if i["schema"] and i["schema"]["type"] in ("integer", "number", "boolean", "enum"):
            q = dict(base)
            q[i["name"]] = "zz"
            st = request(method, path + "?" + urlencode(q), headers=headers)[0]
            check(st == 400, f"{route} query {i['name']} wrong kind: refused ({st})")
        if i["schema"] and i["schema"]["type"] == "enum":
            for v in i["schema"]["enum"]:
                q = dict(base)
                q[i["name"]] = v
                st = request(method, path + "?" + urlencode(q), headers=headers)[0]
                check(st == accept, f"{route} query {i['name']}={v}: accepted ({st})")


def path_probes(route, method, good, bad_by_name, accept, headers=None, body=None):
    sh = shape(route)
    for i in sh["path"]["items"]:
        if not i["schema"] or i["schema"]["type"] not in ("integer", "number", "boolean"):
            continue
        st = request(method, good, headers=headers, body=body)[0]
        check(st == accept, f"{route} path {i['name']} good value accepted ({st})")
        st = request(method, bad_by_name[i["name"]], headers=headers, body=body)[0]
        check(st == 400, f"{route} path {i['name']} wrong kind refused ({st})")


if version == "0.8":
    body_probes("POST /items", "POST", "/v1/items", 201, tok)
    body_probes("POST /form", "POST", "/v1/form", 303, {})
    query_probes("GET /items", "/v1/items", 200)
    query_probes("GET /form", "/v1/form", 200)
    query_probes("GET /r/lower", "/v1/r/lower", 200)
    query_probes("GET /r/q-forms", "/v1/r/q-forms", 200)
    body_probes("HEAD /r/form-head", "HEAD", "/v1/r/form-head", 200, {})
    body_probes("POST /r/camel", "POST", "/v1/r/camel", 200, {})
    body_probes("POST /r/boxed", "POST", "/v1/r/boxed", 202, {})
    body_probes("POST /r/shadow-helper", "POST", "/v1/r/shadow-helper", 200, {})
    path_probes("GET /items/{id}", "GET", "/v1/items/4", {"id": "/v1/items/abc"}, 200)
    path_probes("GET /pair/{a}/{b}", "GET", "/v1/pair/a/4", {"b": "/v1/pair/a/abc"}, 200)
    path_probes("PUT /items/{id}/tags/{tag}", "PUT", "/v1/items/4/tags/t", {"id": "/v1/items/x/tags/t"}, 200)
    st = request("GET", "/v1/whoami")[0]
    check(st == 400, f"GET /whoami: required user-agent left out refused ({st})")
else:
    body_probes("POST /api/users", "POST", "/api/users", 200, {})
    body_probes("PUT /api/users/:id", "PUT", "/api/users/1", 200, {"authorization": "Token abc"})
    query_probes("GET /api/feed", "/api/feed", 200, {"authorization": "Token abc"})
    body_probes("HEAD /api/form-head", "HEAD", "/api/form-head", 200, {})
    path_probes("GET /api/users/:id", "GET", "/api/users/4", {"id": "/api/users/abc"}, 200)

# ---- context (not claims): the forms the shape leaves unknown really vary ----
context = []
if version == "0.8":
    context.append(("serde_as id as a number refused", 400 <= request("POST", "/v1/r/serde-as", body={"id": 5})[0] < 500))
    context.append(("serde_as id as a string accepted", request("POST", "/v1/r/serde-as", body={"id": "5"})[0] == 200))
    context.append(("cfg_attr rename applies", b"someField" in request("GET", "/v1/r/cfg-named")[2]))
    context.append(("nested struct in a query refused", request("GET", "/v1/r/q-forms?limit=1&inner=1")[0] == 400))
    context.append(("u128 in a query refused", request("GET", "/v1/r/q-forms?limit=1&big=1")[0] == 400))
    context.append(("two schemas under 200", b'"a"' in request("GET", "/v1/r/two-json?a=1")[2] and b'"b"' in request("GET", "/v1/r/two-json")[2]))
    context.append(("guarded variant answers 500 for a non-empty message", request("GET", "/v1/r/guard?m=x")[0] == 500))
    context.append(("problem-dyn content type follows the query", request("GET", "/v1/r/problem-dyn?ct=text/x-zz")[1] == "text/x-zz"))
    context.append(("closure shadow answers the closure's body", request("GET", "/v1/r/closure-shadow")[2] == b'{"b":"x"}'))
    context.append(("any-form reads the query on GET", request("GET", "/v1/r/any-form?user=q")[2] == b"q"))
    context.append(("Form on HEAD does not read the query", request("HEAD", "/v1/r/form-head?user=a")[0] == 415))
    context.append(("a repository info! macro reads the query", request("GET", "/v1/r/via-macro")[0] == 400))
    context.append(("via-extract needs the query", request("GET", "/v1/r/via-extract")[0] == 400))
else:
    context.append(("headers-first never runs", request("POST", "/api/headers-first", body={"user": {"username": "a", "email": "e"}})[0] >= 400))
for what, ok in context:
    log.append(("CTX " if ok else "CTXF") + " " + what)
results["context_ok"] = sum(1 for _, ok in context if ok)
results["context_failed"] = sum(1 for _, ok in context if not ok)

proc.terminate()
proc.wait()
for line in log:
    print(line)
print(json.dumps(results))
