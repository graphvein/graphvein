import os


class Settings:
    api_prefix: str = os.environ.get("API_PREFIX", "/api")


settings = Settings()
