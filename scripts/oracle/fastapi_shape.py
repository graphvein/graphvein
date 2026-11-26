"""FastAPI route shape oracle (T029, docs/extract/route-shape.md
"FastAPI").

Imports the FastAPI app of a scratch copy of the route-shape-fastapi
fixture (never corpus code) with the pinned FastAPI 0.161.1 and Pydantic
2.14.2 and writes FastAPI's own app.openapi() document. Run by
scripts/oracle/fastapi_shape.sh with -I -B; TestFastAPIShapeOracle
compares it with the stored route shapes.
"""
import argparse
import importlib
import json
import sys


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", required=True)
    parser.add_argument("--app", required=True, help="module:attribute")
    parser.add_argument("--out", required=True)
    args = parser.parse_args()
    sys.path.insert(0, args.root)
    import fastapi
    import pydantic

    module, attribute = args.app.split(":")
    app = getattr(importlib.import_module(module), attribute)
    result = {"fastapi": fastapi.__version__, "pydantic": pydantic.VERSION, "openapi": app.openapi()}
    with open(args.out, "w") as f:
        json.dump(result, f, indent=1, sort_keys=True)


if __name__ == "__main__":
    main()
