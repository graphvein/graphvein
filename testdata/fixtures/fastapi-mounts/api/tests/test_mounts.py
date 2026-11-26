import uvicorn
from fastapi import FastAPI

from app.main import served_by_test, standalone

# M10: a test-scope mount of a production app does not consume it.
harness = FastAPI()
harness.mount("/t", standalone)

# M21: a test serving a production app does not make it a root.
if __name__ == "__main__":
    uvicorn.run(served_by_test)
