from fastapi import FastAPI

from .models import UserIn, UserOut

app = FastAPI()


@app.post("/users", response_model=UserOut)
def create_user(user: UserIn):
    return UserOut(id=1, name=user.name)


@app.get("/users/{user_id}", response_model=UserOut)
def read_user(user_id: int):
    return UserOut(id=user_id, name="x")
