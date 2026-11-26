from fastapi import APIRouter, FastAPI

child = APIRouter(prefix="/child")
unused = APIRouter(prefix="/unused")
nested = APIRouter(prefix="/nested")
unpacked = APIRouter(prefix="/unpacked")
defaulted = APIRouter(prefix="/defaulted")
decorated = APIRouter(prefix="/decorated")
reassigned = APIRouter(prefix="/reassigned")
prefix_arg = APIRouter(prefix="/prefix-arg")
async_child = APIRouter(prefix="/async")
bare_async = APIRouter(prefix="/bare-async")
sub = FastAPI()

@child.get("")
def empty(): pass

@child.get("/")
def slash(): pass

@unused.get("/x")
def unused_handler(): pass

@nested.get("/x")
def nested_handler(): pass

@unpacked.get("/x")
def unpacked_handler(): pass

@defaulted.get("/x")
def defaulted_handler(): pass

@decorated.get("/x")
def decorated_handler(): pass

@reassigned.get("/x")
def reassigned_handler(): pass

@prefix_arg.get("/x")
def prefix_handler(): pass

@async_child.get("/x")
def async_handler(): pass

@bare_async.get("/x")
def bare_async_handler(): pass

@sub.get("/sub/")
def sub_handler(): pass
