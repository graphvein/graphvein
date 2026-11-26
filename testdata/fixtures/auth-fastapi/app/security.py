"""Security scheme instances (row DA-F3 and its refusals)."""
import os

from fastapi.security import (
    APIKeyCookie,
    APIKeyHeader,
    APIKeyQuery,
    HTTPBasic,
    HTTPBearer,
    OAuth2,
    OAuth2AuthorizationCodeBearer,
    OAuth2PasswordBearer,
    OpenIdConnect,
)
from fastapi.security import http as security_http

FLAG = os.environ.get("AUTO_ERROR") == "1"

# DA-F3: auto_error omitted (the class default, true).
oauth2 = OAuth2PasswordBearer(tokenUrl="token")
# DA-F3: auto_error=False as written.
optional_bearer = HTTPBearer(auto_error=False)
# DA-F3: a literal scheme_name.
header_key = APIKeyHeader(name="x-key", scheme_name="Header key")
# DA-F3 refusal: auto_error is not a literal.
query_key = APIKeyQuery(name="key", auto_error=FLAG)
cookie_key = APIKeyCookie(name="session")
basic = HTTPBasic(realm="fixture")
digest = security_http.HTTPDigest()
code_flow = OAuth2AuthorizationCodeBearer(authorizationUrl="authorize", tokenUrl="token")
openid = OpenIdConnect(openIdConnectUrl="https://example.invalid/.well-known/openid-configuration")
# DA-F3 refusal: scheme_name given positionally.
positional = OAuth2PasswordBearer("token", "Positional")


class CookieBearer(OAuth2PasswordBearer):
    """A subclass without __init__: the call's literals apply."""


# DA-F3: a subclass; its scheme name is the subclass name.
cookie_bearer = CookieBearer(tokenUrl="token", auto_error=False)


class FixedOAuth(OAuth2):
    """A subclass with its own __init__: the settings are not read."""

    def __init__(self):
        super().__init__(auto_error=False)


fixed = FixedOAuth()


class NotAScheme:
    def __call__(self):
        return "value"


# DAD: an instance whose class reaches no fastapi.security scheme.
not_a_scheme = NotAScheme()
