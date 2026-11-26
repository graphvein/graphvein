from fastapi.security import APIKeyHeader, APIKeyQuery, HTTPBasic, HTTPBearer, OAuth2PasswordBearer, OpenIdConnect

TOKEN_URL = "/auth/token"
API_ROOT = "/api"

oauth2 = OAuth2PasswordBearer(tokenUrl=TOKEN_URL)
api_key = APIKeyHeader(name="X-API-Key")
optional_key = APIKeyQuery(name="key", auto_error=False)
bearer = HTTPBearer(bearerFormat="JWT")
basic = HTTPBasic()
oidc = OpenIdConnect(openIdConnectUrl="https://id.example.com/.well-known/openid-configuration")
computed_url = OAuth2PasswordBearer(tokenUrl=f"{API_ROOT}/token", scheme_name="ComputedToken")
