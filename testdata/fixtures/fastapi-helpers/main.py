from fastapi import FastAPI, APIRouter
from children import child, unpacked, sub, async_child, bare_async
from helpers import include as install, include_argument, mount_sub, outer, default_only
from helpers import decorated_helper, reassign, parameter_prefix, awaited_include, nested_definition, via_local

app = FastAPI()
first = APIRouter(prefix="/first")
second = APIRouter(prefix="/second")
app.include_router(first)
app.include_router(second)
install(first)
install(second, enabled=True)
include_argument(target=first, router=child)
if feature:
    include_argument(target=second, router=child)
mount_sub(app, sub)
outer(app)
include_argument(app, *[unpacked])
include_argument(**{"target": app, "router": unpacked})
default_only(app)
decorated_helper(app)
reassign(app)
parameter_prefix(app, "/dynamic-prefix")
nested_definition(app)
awaited_include(app, bare_async)

async def start():
    await awaited_include(app, async_child)

# Each call has its own local router, so target/router pairs must not mix.
via_local(first, child)
via_local(second, async_child)
