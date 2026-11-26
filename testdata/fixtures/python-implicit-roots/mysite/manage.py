from mysite.settings import configure
from polls.api import router
from fastapi import FastAPI

app = FastAPI()
app.include_router(router)

def main():
    configure()
