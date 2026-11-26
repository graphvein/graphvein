from fastapi import FastAPI, APIRouter
app = FastAPI()
router = APIRouter(prefix="/api")
@router.post("/twins")
def empty(): pass
@router.post("/twins/")
def slash(): pass
app.include_router(router)
