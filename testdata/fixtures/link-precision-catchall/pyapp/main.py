from fastapi import FastAPI

app = FastAPI()


@app.get("/t035-py/api/health")
def health():
    return {}


@app.get("/t035-py/api/version")
def version():
    return {}


@app.get("/t035-py/api/{_path:path}")
def reserve(_path: str):
    return {}
