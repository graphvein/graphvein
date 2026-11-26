from fastapi import Depends, FastAPI

from .deps import admin_user
from .routers import admin, items, shared
from .security import code_flow

app = FastAPI()
app.include_router(items.router, prefix="/items")
# include_router dependencies apply to every route of the included router.
app.include_router(admin.router, prefix="/admin", dependencies=[Depends(admin_user)])
app.include_router(shared.router, prefix="/a", dependencies=[Depends(code_flow)])
app.include_router(shared.router, prefix="/b")
