from fastapi import FastAPI

from app.main import scoped

# UP-9 per scope: a test harness includes the production
# router at a known prefix; its test-scope path does not remove the
# production entry below the configured prefix.
harness = FastAPI()
harness.include_router(scoped, prefix="/t")
