import os

from fastapi import FastAPI

app = FastAPI()

# F1 (RO-V1): a mount whose prefix the code does not read is added before
# the route: /health/live is conditional, with assumption RO-V1.
app.mount(os.environ["STATIC"], FastAPI())


@app.get("/health/live")
def health_live():
    return {}
