"""In-process LiteLLM SIWC refresh owner. No CLI or enrollment workflow.

Enrollment and administration are implemented by the Go aginctus CLI.
The managed gateway is the authoritative credential owner.
"""

import contextlib
import fcntl
import hashlib
import json
import math
import os
import re
import stat
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
from pathlib import Path

ISSUER = "https://auth.openai.com"
RESOURCE = "https://api.openai.com/v1"
SCOPES = "openid profile email offline_access resource.invoke chatgpt.tokens.use.direct"
DIRECT = "chatgpt.tokens.use.direct"
NAME = re.compile(r"[a-zA-Z0-9][a-zA-Z0-9_-]{0,63}\Z")
TERMINAL = {"invalid_grant", "invalid_refresh_token", "token_expired",
            "refresh_token_expired", "refresh_token_invalidated", "refresh_token_reused"}
MAX_STATE = 2 << 20


class Error(Exception):
    """Only fixed, credential-free diagnostics cross the CLI/plugin boundary."""


class AuthenticationRequired(Error):
    pass


class OwnerBusy(Error):
    pass


class PlanPermissionRequired(Error):
    pass


class ProviderError(Error):
    def __init__(self, status, code="provider_error"):
        self.status = status
        self.code = code if code in TERMINAL | {"invalid_client"} else "provider_error"
        super().__init__(f"SIWC provider HTTP {status}: {self.code}; retry later or check sign-in")


def valid_name(value):
    if not isinstance(value, str) or not NAME.fullmatch(value):
        raise Error("Use a simple account or route label (letters, digits, hyphen, underscore)")
    return value


def private_read(path, limit=MAX_STATE):
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    with os.fdopen(fd, "rb") as file:
        info = os.fstat(file.fileno())
        if (not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o600
                or info.st_uid != os.geteuid() or info.st_nlink != 1):
            raise Error("SIWC files must be owned, single-link regular files with mode 0600")
        data = file.read(limit + 1)
        if len(data) > limit:
            raise Error("SIWC file exceeds size limit")
        return data


class Store:
    def __init__(self, root):
        self.root = Path(root)
        if not self.root.is_absolute() or str(self.root) != os.path.abspath(self.root):
            raise Error("SIWC state directory must be absolute and normalized")
        # Ancestors are trusted operator-controlled paths, as with filestate.
        self.root.mkdir(mode=0o700, parents=True, exist_ok=True)
        info = self.root.lstat()
        if (not stat.S_ISDIR(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o700
                or info.st_uid != os.geteuid()):
            raise Error("SIWC state directory must be owned, non-symlink and mode 0700")

    @contextlib.contextmanager
    def lock(self, name="state", timeout=20, shared=False):
        path = self.root / (name + ".flock")
        fd = os.open(path, os.O_CREAT | os.O_RDWR | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC, 0o600)
        with os.fdopen(fd, "rb") as file:
            info = os.fstat(file.fileno())
            if (not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o600
                    or info.st_uid != os.geteuid() or info.st_nlink != 1):
                raise Error("Unsafe SIWC lock file")
            deadline = time.monotonic() + timeout
            while True:
                try:
                    fcntl.flock(file, (fcntl.LOCK_SH if shared else fcntl.LOCK_EX) | fcntl.LOCK_NB)
                    break
                except BlockingIOError:
                    if time.monotonic() >= deadline:
                        raise OwnerBusy("SIWC owner busy; retry apply or provider login after active requests finish") from None
                    time.sleep(0.05)
            if not os.path.samestat(info, path.lstat()):
                raise Error("SIWC lock was replaced")
            yield

    def read(self):
        try:
            data = json.loads(private_read(self.root / "state.json"))
        except FileNotFoundError:
            return {"version": 1, "host_id": "urn:uuid:" + str(uuid.uuid4()), "accounts": {}, "routes": {}}
        except (ValueError, TypeError):
            raise Error("Malformed SIWC state; restore protected state or re-enroll") from None
        return self.validate(data)

    @staticmethod
    def validate(data):
        if (not isinstance(data, dict) or data.get("version") != 1
                or not isinstance(data.get("accounts"), dict) or not isinstance(data.get("routes"), dict)
                or not isinstance(data.get("host_id"), str)
                or not data["host_id"].startswith("urn:uuid:")):
            raise Error("Invalid SIWC state format")
        try:
            uuid.UUID(data["host_id"][9:])
        except ValueError:
            raise Error("Invalid SIWC host identity") from None
        try:
            for label, record in data["accounts"].items():
                valid_name(label)
                if (not isinstance(record, dict) or not isinstance(record.get("client_id"), str)
                        or not re.fullmatch(r"[A-Za-z0-9_-]{1,256}", record["client_id"])
                        or record["client_id"] == "dynamic_agent_client"):
                    raise Error("Invalid SIWC account registration")
                if record.get("access_token") or record.get("refresh_token"):
                    if (any(not isinstance(record.get(k), str) or not record[k]
                            for k in ("sub", "access_token", "refresh_token"))
                            or not isinstance(record.get("scopes"), list)
                            or any(not isinstance(scope, str) for scope in record["scopes"])
                            or type(record.get("expires_at")) not in (int, float)
                            or not math.isfinite(record["expires_at"])
                            or type(record.get("earliest_refresh_at", 0)) not in (int, float)
                            or not math.isfinite(record.get("earliest_refresh_at", 0))):
                        raise Error("Invalid persisted SIWC credentials; reauthorize")
            for alias, route in data["routes"].items():
                valid_name(alias)
                if (not alias.startswith("siwc-") or not isinstance(route, dict)
                        or route["account"] not in data["accounts"]
                        or not isinstance(route["model"], str) or not re.fullmatch(r"[A-Za-z0-9._-]+", route["model"])
                        or not isinstance(route["key_hashes"], list) or not route["key_hashes"]
                        or any(not isinstance(key, str) for key in route["key_hashes"])
                        or route["consent_background"] is not True or route["consent_connected_app"] is not True
                        or route["client_id"] != data["accounts"][route["account"]]["client_id"]
                        or route["sub"] != data["accounts"][route["account"]].get("sub")):
                    raise Error("Invalid persisted SIWC route binding")
        except (KeyError, TypeError):
            raise Error("Malformed SIWC account or route state") from None
        return data

    def write(self, data, name="state.json"):
        if name == "state.json":
            self.validate(data)
        elif name == "pending.json":
            self.validate(data["state"])
        else:
            raise Error("Invalid managed SIWC state filename")
        target = self.root / name
        if target.exists() or target.is_symlink():
            private_read(target)
        fd, path = tempfile.mkstemp(prefix=".siwc-", dir=self.root)
        try:
            with os.fdopen(fd, "wb") as file:
                file.write(json.dumps(data, allow_nan=False).encode())
                file.flush()
                os.fsync(file.fileno())
            os.replace(path, target)
            parent = os.open(self.root, os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
            try:
                os.fsync(parent)
            finally:
                os.close(parent)
        finally:
            if os.path.exists(path):
                os.unlink(path)

    def accept_pending(self):
        """Publish a stopped-instance update while holding the runtime lease."""
        path = self.root / "pending.json"
        with self.lock():
            try:
                pending = json.loads(private_read(path))
            except FileNotFoundError:
                self.write(self.read())
                return
            except (ValueError, TypeError):
                raise Error("Incomplete managed SIWC update; run provider login again") from None
            try:
                state = self.validate(pending["state"])
                current = private_read(self.root / "state.json")
                matches = hashlib.sha256(current).hexdigest() == pending["base_digest"]
                if not matches and self.read() != state:
                    raise Error("Stale managed SIWC update rejected")
            except (KeyError, TypeError):
                raise Error("Invalid managed SIWC update") from None
            self.write(state)
            path.unlink()
            parent = os.open(self.root, os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
            try:
                os.fsync(parent)
            finally:
                os.close(parent)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


def request_json(url, form=None, bearer=None):
    """Fixed HTTPS destinations only; no inherited proxy or redirect credentials."""
    parsed = urllib.parse.urlsplit(url)
    if parsed.scheme != "https" or parsed.hostname not in {"auth.openai.com", "api.openai.com"}:
        raise Error("Unsafe SIWC provider destination")
    headers = {"Accept": "application/json"}
    body = None
    if form is not None:
        body = urllib.parse.urlencode(form).encode()
        headers["Content-Type"] = "application/x-www-form-urlencoded"
    if bearer:
        headers["Authorization"] = "Bearer " + bearer
    request = urllib.request.Request(url, body, headers)
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())
    try:
        with opener.open(request, timeout=15) as response:
            raw = response.read(MAX_STATE + 1)
            if len(raw) > MAX_STATE:
                raise Error("Oversized SIWC provider response")
            return json.loads(raw) if raw else {}
    except urllib.error.HTTPError as exc:
        try:
            data = json.loads(exc.read(65536))
            code = data.get("error", "") if isinstance(data, dict) else ""
        except (ValueError, OSError):
            code = ""
        finally:
            exc.close()
        raise ProviderError(exc.code, code if isinstance(code, str) else "") from None
    except (OSError, ValueError):
        raise Error("SIWC network or response failure; credentials preserved; check connectivity") from None


class Provider:
    def __init__(self, request=request_json, clock=time.time):
        try:
            import jwt
            if "RS256" not in jwt.algorithms.get_default_algorithms():
                raise ImportError
        except ImportError:
            raise Error("SIWC requires PyJWT with cryptography; use the packaged Python environment") from None
        self.request = request
        self.clock = clock

    def discovery(self):
        data = self.request(ISSUER + "/.well-known/openid-configuration")
        if not isinstance(data, dict) or data.get("issuer") != ISSUER:
            raise Error("SIWC discovery issuer mismatch")
        for key in ("jwks_uri", "revocation_endpoint"):
            url = urllib.parse.urlsplit(data.get(key, ""))
            if (url.scheme != "https" or url.netloc != "auth.openai.com"
                    or url.fragment or url.query):
                raise Error("Unsafe SIWC discovery endpoint")
        return data

    def claims(self, token, audience, nonce=None):
        import jwt

        try:
            header = jwt.get_unverified_header(token)
            if header.get("alg") != "RS256" or not isinstance(header.get("kid"), str):
                raise Error("Unsupported SIWC identity signature")
            keys = self.request(self.discovery()["jwks_uri"])["keys"]
            matches = [k for k in keys if k.get("kid") == header["kid"] and k.get("kty") == "RSA"
                       and k.get("use", "sig") == "sig" and k.get("alg", "RS256") == "RS256"]
            if len(matches) != 1:
                raise Error("SIWC signing key not found or ambiguous")
            key = jwt.PyJWK.from_dict(matches[0], algorithm="RS256").key
            claims = jwt.decode(token, key, algorithms=["RS256"], audience=audience, issuer=ISSUER,
                                options={"require": ["exp", "iat", "iss", "sub", "aud"],
                                         "verify_exp": False, "verify_iat": False, "verify_nbf": False})
            now = self.clock()
            if (not isinstance(claims.get("sub"), str) or not claims["sub"]
                    or type(claims["exp"]) not in (int, float) or not math.isfinite(claims["exp"])
                    or claims["exp"] <= now or type(claims["iat"]) not in (int, float)
                    or not math.isfinite(claims["iat"]) or claims["iat"] > now + 30
                    or ("nbf" in claims and (type(claims["nbf"]) not in (int, float)
                                            or not math.isfinite(claims["nbf"]) or claims["nbf"] > now))
                    or (nonce is not None and claims.get("nonce") != nonce)
                    or (isinstance(claims["aud"], list) and len(claims["aud"]) > 1
                        and claims.get("azp") != audience)):
                raise Error("SIWC identity claims rejected")
            return claims
        except Error:
            raise
        except Exception:
            raise Error("SIWC token signature or claims rejected") from None

    def tokens(self, form, previous=None, nonce=None):
        data = self.request(ISSUER + "/api/accounts/oauth/token", form=form)
        if (not isinstance(data, dict) or any(not isinstance(data.get(k), str) or not data[k]
                for k in ("access_token", "refresh_token", "token_type"))
                or data["token_type"].lower() != "bearer" or type(data.get("expires_in")) is not int
                or not 0 < data["expires_in"] <= 86400):
            raise Error("Incomplete SIWC token response; sign in again if rotation occurred")
        scopes = data.get("scope", " ".join(previous.get("scopes", [])) if previous else "")
        if not isinstance(scopes, str):
            raise Error("Malformed SIWC grant")
        identity = previous
        if "id_token" in data:
            identity = self.claims(data["id_token"], form["client_id"], nonce)
        elif nonce is not None:
            raise Error("SIWC enrollment requires a validated ID token")
        if not identity or (previous and identity["sub"] != previous["sub"]):
            raise Error("SIWC reauthorization returned another account; original preserved")
        access = self.claims(data["access_token"], RESOURCE)
        if access.get("client_id") != form["client_id"] or access["sub"] != identity["sub"]:
            raise Error("SIWC access token does not match the selected registration")
        granted = scopes.split()
        if not set(granted).issubset(set(str(access.get("scope", "")).split())):
            raise Error("SIWC token and response scopes disagree")
        earliest = data.get("earliest_refresh_at", 0)
        if type(earliest) not in (int, float) or not math.isfinite(earliest):
            raise Error("Invalid SIWC refresh timing")
        return {"client_id": form["client_id"], "sub": identity["sub"],
                "email": identity.get("email", ""), "access_token": data["access_token"],
                "refresh_token": data["refresh_token"],
                "id_token": data.get("id_token", previous.get("id_token", "") if previous else ""),
                "scopes": granted, "expires_at": min(self.clock() + data["expires_in"], access["exp"]),
                "earliest_refresh_at": earliest}


class Owner:
    def __init__(self, store, provider=None):
        self.store = store
        self.provider = provider or Provider()

    def access(self, account):
        with self.store.lock():
            state = self.store.read()
            record = state["accounts"].get(valid_name(account))
            if not record or not record.get("refresh_token"):
                raise AuthenticationRequired("SIWC account is signed out; stop the gateway and sign in again")
            if record.get("refresh_pending"):
                raise AuthenticationRequired("SIWC refresh outcome uncertain; stop the gateway and reauthorize")
            if DIRECT not in record.get("scopes", []):
                raise PlanPermissionRequired("ChatGPT plan usage was not granted; explicitly enable it by signing in again")
            now = self.provider.clock()
            if now < record["expires_at"] - 60 or (now < record["expires_at"]
                    and now < record.get("earliest_refresh_at", 0)):
                return record["access_token"]
            if now < record.get("earliest_refresh_at", 0):
                raise Error("SIWC access expired before provider refresh window; retry later")
            # A crash or an ambiguous network result must never reuse a potentially
            # consumed rotating token. A durable marker makes recovery explicit.
            record["refresh_pending"] = True
            self.store.write(state)
            try:
                replacement = self.provider.tokens({"grant_type": "refresh_token", "client_id": record["client_id"],
                                                     "refresh_token": record["refresh_token"], "resource": RESOURCE}, record)
            except ProviderError as exc:
                record.pop("refresh_pending", None)
                if exc.code in TERMINAL:
                    clear_tokens(record)
                self.store.write(state)
                raise
            state["accounts"][account] = replacement
            self.store.write(state)
            if DIRECT not in replacement["scopes"]:
                raise PlanPermissionRequired("ChatGPT plan usage is disabled on the refreshed grant")
            return replacement["access_token"]

def clear_tokens(record):
    for name in ("access_token", "refresh_token", "id_token", "expires_at", "earliest_refresh_at", "refresh_pending"):
        record.pop(name, None)
