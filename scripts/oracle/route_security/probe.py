"""Route security oracle probe (T029).

Reads the claims Graphvein stored for the route-shape-security fixture
(claims.json from TestRouteSecurityExport) and sends real requests to the
fixture apps served on 127.0.0.1: for every requirement whose schemes are
complete, a request with exactly that requirement's credentials must reach
the handler (200); for a fact requirement, a request without credentials
and, for a requirement of several schemes, a request missing one of them
must not (any status but 200). Paths the oracle lists as served without
any credential (oracle.json "open") must answer 200 and no claim for them,
or for a route listed in "open_routes", may hold a fact requirement.
Prints one line per check and a summary.

It also compares the handler count gin prints for each route (debug
mode) with the stored middleware: the route's own middleware, its
router-level middleware (group_middleware) and the handler; a route with
group_middleware_unknown entries may count more, never fewer.

Usage: python3 -I probe.py CLAIMS ORACLE_JSON SERVER_LOG app=port...
"""
import base64
import hashlib
import hmac
import json
import re
import sys
import time
import urllib.error
import urllib.parse
import urllib.request


def b64(data):
    return base64.urlsafe_b64encode(data).rstrip(b"=").decode()


def jwt(cfg):
    now = int(time.time())
    header = b64(json.dumps({"alg": "HS256", "typ": "JWT"}).encode())
    payload = b64(json.dumps({"sub": "fixture", "iss": cfg["issuer"], "aud": cfg["audience"], "iat": now, "exp": now + 3600}).encode())
    sig = hmac.new(cfg["secret"].encode(), f"{header}.{payload}".encode(), hashlib.sha256).digest()
    return f"{header}.{payload}.{b64(sig)}"


def send(url, headers):
    req = urllib.request.Request(url, headers=headers, method="GET")
    try:
        with urllib.request.urlopen(req, timeout=5) as r:
            return r.status
    except urllib.error.HTTPError as e:
        return e.code
    except TimeoutError:
        return "timeout"


def main():
    claims = json.load(open(sys.argv[1]))
    cfg = json.load(open(sys.argv[2]))
    log = open(sys.argv[3]).read()
    ports = dict(a.split("=", 1) for a in sys.argv[4:])
    token = jwt(cfg)
    counts = {"agree": 0, "disagree": 0, "untestable": 0, "not_served": 0}

    def app_of(route):
        file = route.split(":", 1)[1].split("#", 1)[0]
        for prefix, app in cfg["apps"].items():
            if file.startswith(prefix):
                return app
        return None

    def creds(route, scheme, url):
        """Headers and URL for one scheme, or None when not complete."""
        headers = {}
        if scheme["type"] == "http" and scheme["scheme"] == "bearer":
            headers["Authorization"] = "Bearer " + cfg["bearer"].get(route, token)
        elif scheme["type"] == "http" and scheme["scheme"] == "basic":
            pair = f'{cfg["basic"]["user"]}:{cfg["basic"]["password"]}'.encode()
            headers["Authorization"] = "Basic " + base64.b64encode(pair).decode()
        elif scheme["type"] == "apiKey" and scheme.get("param_name"):
            name, where = scheme["param_name"], scheme["in"]
            value = cfg["bearer"].get(route, token)
            if where == "header":
                headers[name] = value
            elif where == "cookie":
                headers["Cookie"] = f"{name}={value}"
            elif where == "query":
                sep = "&" if "?" in url else "?"
                url = url + sep + urllib.parse.urlencode({name: value})
        else:
            return None
        return headers, url

    def unread(c):
        reason = c.get("reason") or ""
        return "is not read for auth" in reason or "may run before the route" in reason

    def check(ok, text):
        counts["agree" if ok else "disagree"] += 1
        print(("OK   " if ok else "FAIL ") + text)

    gin = {}
    for m in re.finditer(r"\[GIN-debug\] (\w+)\s+(\S+)\s+--> \S+ \((\d+) handlers\)", log):
        gin[(m.group(1), m.group(2))] = int(m.group(3))
    for c in claims:
        c["paths"] = c["paths"] or []
    for c in claims:
        if app_of(c["route"]) != "gin":
            continue
        for path in c["paths"]:
            got = gin.get((c["method"], path))
            want = c["route_middleware"] + c["group_middleware"] + 1
            if got is None:
                check(False, f"{c['route']} {path}: gin did not register it")
            elif c["group_middleware_unknown"]:
                check(got >= want, f"{c['route']} {path}: gin runs {got} handlers, stored {want} and {c['group_middleware_unknown']} unknown")
            else:
                check(got == want, f"{c['route']} {path}: gin runs {got} handlers, stored {want}")

    for c in claims:
        if not c["requirements"]:
            continue
        app = app_of(c["route"])
        if app is None or app not in ports:
            counts["not_served"] += 1
            print("SKIP not served " + c["route"])
            continue
        base = f"http://127.0.0.1:{ports[app]}"
        schemes = {s["name"]: s for s in c["schemes"]}
        for path in c["paths"]:
            url0 = base + path
            all_fact = all(r["provenance"]["kind"] == "fact" for r in c["requirements"])
            for r in c["requirements"]:
                names = [u["name"] for u in r["schemes"]]
                headers, url, testable = {}, url0, True
                parts = []
                for n in names:
                    got = creds(c["route"], schemes[n], url)
                    if got is None:
                        testable = False
                        break
                    h, url = got
                    parts.append(h)
                    headers.update(h)
                if not testable:
                    counts["untestable"] += 1
                    print(f"SKIP untestable {c['route']} {path} requires {names}")
                    continue
                status = send(url, headers)
                text = f"{c['route']} {path} with {names}: {status}"
                if r["provenance"]["kind"] != "fact":
                    # Not claimed: an intended requirement may not be enforced.
                    print(f"INFO {text} (intended, not claimed)")
                elif unread(c):
                    # Middleware no row reads may refuse too: acceptance is
                    # not claimed, only that the credential is required.
                    print(f"INFO {text} (other middleware not read, not claimed)")
                else:
                    check(status == 200, text)
                if r["provenance"]["kind"] == "fact" and len(names) > 1:
                    # Every scheme of one requirement applies together.
                    for i, n in enumerate(names):
                        rest = {}
                        u = url0
                        for j, m in enumerate(names):
                            if j != i:
                                h, u = creds(c["route"], schemes[m], u)
                                rest.update(h)
                        status = send(u, rest)
                        check(status != 200, f"{c['route']} {path} without {n}: {status} (refused)")
            if all_fact:
                status = send(url0, {})
                check(status != 200, f"{c['route']} {path} without credentials: {status} (refused)")
            else:
                print(f"INFO {c['route']} {path} without credentials: {send(url0, {})} (not claimed)")
    # Paths the real framework serves without any credential (forms where
    # a stated middleware does not run): no claim may hold a fact
    # requirement for them.
    for app, paths in cfg.get("open", {}).items():
        for path in paths:
            status = send(f"http://127.0.0.1:{ports[app]}{path}", {})
            check(status == 200, f"{app} {path} served without credentials: {status}")
            facts = [c["route"] for c in claims if app_of(c["route"]) == app and path in c["paths"]
                     and any(r["provenance"]["kind"] == "fact" for r in c["requirements"])]
            check(not facts, f"{app} {path}: no fact requirement claimed {facts}")
    # Routes whose path another registration takes (no path is claimed for
    # them): the paths above show the real answer; no fact either.
    for route in cfg.get("open_routes", []):
        facts = [c for c in claims if c["route"] == route and any(r["provenance"]["kind"] == "fact" for r in c["requirements"])]
        check(any(c["route"] == route for c in claims) and not facts, f"{route}: no fact requirement claimed")
    print(f"summary: {counts['agree']} agree, {counts['disagree']} disagree, {counts['untestable']} requirements untestable, {counts['not_served']} routes not served")
    return 1 if counts["disagree"] else 0


if __name__ == "__main__":
    sys.exit(main())
