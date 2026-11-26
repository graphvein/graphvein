from fastapi import FastAPI
app = FastAPI()
@app.get('/t012c/new/t012c/hey-items/{id}')
def item(id: str): pass
@app.post('/t012c/new/t012c/hey-items/')
def slash(): pass
@app.get('/t012c/old/t012c/hey-items/{id}')
def old_item(id: str): pass
@app.get('/t012c/per-call/t012c/hey-items')
def per_call(): pass
@app.get('/t012c/preserved/t012c/hey-items')
def preserved(): pass
