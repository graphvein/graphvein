from fastapi import Depends, FastAPI

from app.routers import admin, items, review
from app.routers.admin import tenant_header

app = FastAPI()
app.include_router(items.router)
app.include_router(admin.router, dependencies=[Depends(tenant_header)])
app.include_router(review.router)
app.include_router(review.custom)
