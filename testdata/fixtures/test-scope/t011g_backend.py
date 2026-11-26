from fastapi import FastAPI, APIRouter
app = FastAPI()
child = APIRouter()

@app.get('/t011g/mixed')
def mixed(): pass

@app.api_route('/t011g/only', methods=[])
def weaker(): pass

@app.get('/t011g/two')
def first(): pass

@app.get('/t011g/two')
def second(): pass

@child.get('/child')
def child_route(): pass

app.include_router(child, prefix='/t011g/prod')

def install(target):
    target.include_router(child, prefix='/t011g/helper')

install(app)

from tests.t011g_helpers import include_from_test
include_from_test(app, child)
