from fastapi import FastAPI
app = FastAPI()
@app.get("/t013b/slash")
def plain(): pass
@app.get("/t013b/slash/")
def slash(): pass
