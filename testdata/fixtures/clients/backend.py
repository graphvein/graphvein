from fastapi import FastAPI
app = FastAPI()
@app.get('/api/python-head')
def python_handler():
    pass
@app.head('/api/python-explicit')
def explicit_handler():
    pass
