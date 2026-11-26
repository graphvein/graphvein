from fastapi import FastAPI
from t011g_backend import app, child, install
test_app = FastAPI()

@test_app.get('/t011g/mixed')
def test_mixed(): pass

@test_app.get('/t011g/only')
def only(): pass

@test_app.get('/t011g/two')
def two(): pass

@app.get('/t011g/registered')
def registered(): pass

app.include_router(child, prefix='/t011g/test')
install(test_app)
install(app)
