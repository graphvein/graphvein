"""FastAPI security oracle (T026, docs/extract/auth.md "Oracle").

Imports the named FastAPI apps of a scratch copy of the auth-fastapi
fixture (never corpus code) with the pinned FastAPI 0.161.1 and writes,
per app, every operation's `security` as FastAPI's own app.openapi()
computes it. Run by scripts/oracle/fastapi_security.sh with -I -B.
"""
import argparse
import importlib
import json
import sys


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", required=True)
    parser.add_argument("--apps", required=True, help="module:attribute,...")
    parser.add_argument("--out", required=True)
    args = parser.parse_args()
    sys.path.insert(0, args.root)
    import fastapi

    result = {"fastapi": fastapi.__version__, "apps": {}}
    for spec in args.apps.split(","):
        module, attribute = spec.split(":")
        app = getattr(importlib.import_module(module), attribute)
        operations = {}
        for path, item in sorted(app.openapi().get("paths", {}).items()):
            for method, operation in sorted(item.items()):
                names = []
                for requirement in operation.get("security", []):
                    names.extend(requirement.keys())
                operations[method.upper() + " " + path] = names
        result["apps"][spec] = operations
    with open(args.out, "w") as f:
        json.dump(result, f, indent=1, sort_keys=True)


if __name__ == "__main__":
    main()
