from pydantic_settings import BaseSettings

class Settings(BaseSettings):
    API: str = "/api"

settings = Settings(_env_file=None)
overridden = Settings(API="/override")
