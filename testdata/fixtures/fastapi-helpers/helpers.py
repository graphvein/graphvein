from children import child, unused, nested, defaulted, decorated, reassigned, prefix_arg
from fastapi import FastAPI

def include(target, /, *, enabled=True):
    if enabled:
        from children import child as local_child
        target.include_router(local_child, prefix="/included")

def include_argument(target, router):
    target.include_router(router, prefix="/argument")

def mount_sub(target, subapp):
    target.mount("/mounted/", subapp)

def uncalled(target):
    target.include_router(unused)

def inner(target):
    target.include_router(nested)

def outer(target):
    inner(target)
    target = FastAPI()

def default_only(target, router=defaulted):
    target.include_router(router)

def identity(f):
    return f

@identity
def decorated_helper(target):
    target.include_router(decorated)

def reassign(target):
    target = object()
    target.include_router(reassigned)

def parameter_prefix(target, prefix):
    target.include_router(prefix_arg, prefix=prefix)

async def awaited_include(target, router):
    target.include_router(router)

def nested_definition(target):
    def local_helper(value):
        value.include_router(unused)
    local_helper(target)

def via_local(target, router):
    from fastapi import APIRouter
    local = APIRouter(prefix="/local")
    intermediate = APIRouter(prefix="/deep")
    intermediate.include_router(router)
    local.include_router(intermediate)
    target.include_router(local)
