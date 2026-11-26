from fastapi import Depends, FastAPI

from .security import openid

# App-level dependencies apply to the app's own routes...
app = FastAPI(dependencies=[Depends(openid)])
# ...but not to a mounted sub-app, which starts its own chain.
sub = FastAPI()
app.mount("/sub", sub)


@app.get("/partner")
def partner():
    return {}


@sub.get("/inside")
def inside():
    return {}
