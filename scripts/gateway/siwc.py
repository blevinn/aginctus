"""Aginctus SIWC enrollment and credential owner. No inference listener.

The same source runs on the trusted enrollment host and inside LiteLLM.
See docs/gateway-siwc.md for ownership transfer and the connected-app boundary.
"""

import argparse
import base64
import contextlib
import fcntl
import hashlib
import http.server
import json
import math
import os
import re
import secrets
import stat
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import uuid
import webbrowser
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
    def lock(self, name="state", timeout=20):
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
                    fcntl.flock(file, fcntl.LOCK_EX | fcntl.LOCK_NB)
                    break
                except BlockingIOError:
                    if time.monotonic() >= deadline:
                        raise Error("SIWC owner busy; stop LiteLLM before enrollment, transfer or logout") from None
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

    def write(self, data):
        target = self.root / "state.json"
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

    def models(self, account):
        data = self.provider.request(RESOURCE + "/models", bearer=self.access(account))
        if not isinstance(data, dict) or not isinstance(data.get("models"), list):
            raise Error("Invalid SIWC model catalog")
        return [{"slug": item["slug"], "display_name": item.get("display_name", item["slug"])}
                for item in data["models"] if isinstance(item, dict) and item.get("visibility") == "list"
                and isinstance(item.get("slug"), str) and re.fullmatch(r"[A-Za-z0-9._-]+", item["slug"])]

    def logout(self, account):
        with self.store.lock("runtime", timeout=0), self.store.lock():
            state = self.store.read()
            record = state["accounts"].get(valid_name(account))
            if not record:
                raise Error("Unknown SIWC account")
            confirmed = not record.get("refresh_token")
            if not confirmed:
                for attempt in range(3):
                    try:
                        endpoint = self.provider.discovery()["revocation_endpoint"]
                        self.provider.request(endpoint, form={"token": record["refresh_token"],
                                              "token_type_hint": "refresh_token", "client_id": record["client_id"]})
                        confirmed = True
                        break
                    except ProviderError as exc:
                        if exc.status < 500:
                            break
                    except Error:
                        pass
                    if attempt < 2:
                        time.sleep(0.2 * (2 ** attempt))
            clear_tokens(record)
            self.store.write(state)
            return confirmed

    def export(self, account, target_host, path):
        if not target_host.startswith("urn:uuid:"):
            raise Error("Initialize the target SIWC store and supply its host ID")
        try:
            uuid.UUID(target_host[9:])
        except ValueError:
            raise Error("Invalid target host ID") from None
        destination = Path(path)
        if not destination.is_absolute() or destination.parent != self.store.root:
            raise Error("Write the handoff capsule directly inside the private SIWC state directory")
        if destination.name in {"state.json", "state.flock", "runtime.flock"} or destination.exists() or destination.is_symlink():
            raise Error("SIWC handoff destination must be a new capsule file")
        with self.store.lock("runtime", timeout=0), self.store.lock():
            state = self.store.read()
            record = state["accounts"].get(valid_name(account))
            if not record or not record.get("refresh_token") or record.get("refresh_pending"):
                raise Error("No renewable SIWC session available for exclusive transfer")
            capsule = {"version": 1, "handoff_id": secrets.token_hex(32), "host_id": target_host,
                       "account": account, "record": dict(record),
                       "routes": {k: v for k, v in state["routes"].items() if v["account"] == account}}
            fd = os.open(destination, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
            with os.fdopen(fd, "wb") as file:
                # Reserve an empty destination before disarming so a collision or
                # permission error cannot discard a working session. No capsule
                # is usable until the source's removal has been fsynced.
                clear_tokens(record)
                self.store.write(state)
                file.write(json.dumps(capsule).encode())
                file.flush()
                os.fsync(file.fileno())

    def receive(self, path):
        with self.store.lock("runtime", timeout=0), self.store.lock():
            state = self.store.read()
            try:
                capsule = json.loads(private_read(Path(path)))
                account = valid_name(capsule["account"])
                handoff = capsule["handoff_id"]
                if (capsule["version"] != 1 or capsule["host_id"] != state["host_id"]
                        or not re.fullmatch(r"[a-f0-9]{64}", handoff)
                        or handoff in state.get("received", []) or account in state["accounts"]
                        or not isinstance(capsule["routes"], dict)
                        or set(capsule["routes"]).intersection(state["routes"])):
                    raise Error("SIWC handoff target, replay or account collision")
                record = capsule["record"]
                # Verify the transferred access/identity claims instead of trusting
                # a different credential format or caller-supplied identity fields.
                access = self.provider.claims(record["access_token"], RESOURCE)
                identity = self.provider.claims(record["id_token"], record["client_id"])
                if (identity["sub"] != record["sub"] or access["sub"] != record["sub"]
                        or access.get("client_id") != record["client_id"]
                        or not record.get("refresh_token") or record.get("refresh_pending")
                        or record["expires_at"] > access["exp"]):
                    raise Error("SIWC handoff credentials do not match their registration")
                for alias, route in capsule["routes"].items():
                    valid_name(alias)
                    if (route["account"] != account or route["client_id"] != record["client_id"]
                            or route["sub"] != record["sub"]):
                        raise Error("SIWC handoff route binding mismatch")
            except (KeyError, ValueError, TypeError):
                raise Error("Malformed SIWC handoff capsule") from None
            state["accounts"][account] = record
            state["routes"].update(capsule["routes"])
            state.setdefault("received", []).append(handoff)
            self.store.write(state)


def clear_tokens(record):
    for name in ("access_token", "refresh_token", "id_token", "expires_at", "earliest_refresh_at", "refresh_pending"):
        record.pop(name, None)


class Callback(http.server.BaseHTTPRequestHandler):
    def log_message(self, *args):
        pass

    def do_GET(self):
        self.close_connection = True
        parsed = urllib.parse.urlsplit(self.path)
        query = urllib.parse.parse_qs(parsed.query, keep_blank_values=True)
        expected = self.server.attempt
        good = (len(self.path) <= 16384 and not parsed.scheme and not parsed.netloc and parsed.path == "/auth/callback"
                and self.headers.get("Host") == expected["host"]
                and all(len(query.get(k, [])) <= 1 for k in ("state", "code", "client_id", "error"))
                and secrets.compare_digest(query.get("state", [""])[0].encode(), expected["state"].encode()))
        if not good or self.server.result is not None:
            self.send_error(400, "Invalid or replayed SIWC callback")
            return
        self.server.result = {key: values[0] for key, values in query.items()}
        self.send_response(200)
        self.send_header("Cache-Control", "no-store")
        self.send_header("Referrer-Policy", "no-referrer")
        self.end_headers()
        self.wfile.write(b"Return to Aginctus. Sign-in is being validated.\n")


class CallbackServer(http.server.HTTPServer):
    def get_request(self):
        connection, address = super().get_request()
        connection.settimeout(2)
        return connection, address


def login(owner, account, open_browser=webbrowser.open, output=sys.stdout, timeout=300, enable_plan=False):
    valid_name(account)
    with owner.store.lock("runtime", timeout=0), owner.store.lock(timeout=1):
        state = owner.store.read()
        owner.store.write(state)  # Persist the host ID before browser authorization.
        previous = state["accounts"].get(account)
        client_id = previous["client_id"] if previous else "dynamic_agent_client"
        verifier = secrets.token_urlsafe(48)
        nonce = secrets.token_urlsafe(32)
        with CallbackServer(("127.0.0.1", 0), Callback) as server:
            server.timeout = 0.2
            host = "127.0.0.1:" + str(server.server_port)
            redirect = "http://" + host + "/auth/callback"
            server.attempt = {"host": host, "state": secrets.token_urlsafe(32)}
            server.result = None
            params = {"client_id": client_id, "ext_agent_host_id": state["host_id"], "response_type": "code",
                      "redirect_uri": redirect, "scope": SCOPES, "resource": RESOURCE, "state": server.attempt["state"],
                      "nonce": nonce, "code_challenge_method": "S256",
                      "code_challenge": base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).decode().rstrip("=")}
            if not previous:
                params["agent_name_hint"] = "Aginctus"
            if enable_plan:
                params["prompt"] = "consent"
            # Omit retained ID-token hints: no secret in terminal URLs or browser argv.
            address = ISSUER + "/api/accounts/authorize?" + urllib.parse.urlencode(params)
            print("Continue with ChatGPT. Use the account/workspace for this Aginctus registration:\n" + address,
                  file=output, flush=True)
            open_browser(address)
            deadline = time.monotonic() + timeout
            while server.result is None:
                if time.monotonic() >= deadline:
                    raise Error("SIWC sign-in timed out; saved credentials preserved")
                server.handle_request()
            result = server.result
        if result.get("error"):
            raise Error("SIWC authorization declined; saved credentials preserved")
        issued = result.get("client_id", client_id)
        if (not result.get("code") or issued == "dynamic_agent_client"
                or not re.fullmatch(r"[A-Za-z0-9_-]{1,256}", issued)
                or (previous and issued != client_id)):
            raise Error("SIWC callback is incomplete or returned another client")
        if not previous:
            # Retain the issued registration even if exchange fails, without claiming
            # an authenticated identity or replacing any other account's credentials.
            state["accounts"][account] = {"client_id": issued}
            owner.store.write(state)
        record = owner.provider.tokens({"grant_type": "authorization_code", "client_id": issued,
                                       "code": result["code"], "code_verifier": verifier,
                                       "redirect_uri": redirect, "resource": RESOURCE},
                                      previous if previous and previous.get("sub") else None, nonce)
        for label, existing in state["accounts"].items():
            if label != account and existing.get("client_id") == record["client_id"]:
                raise Error("SIWC registration already belongs to another label")
        state["accounts"][account] = record
        owner.store.write(state)
        print("SIWC sign-in saved. " + ("ChatGPT plan usage enabled." if DIRECT in record["scopes"]
                                       else "Identity only: ChatGPT plan usage was not granted."), file=output)


def configure(owner, args):
    if not args.consent_connected_app or not args.consent_background:
        raise Error("Route binding requires connected-application and background-use consent")
    valid_name(args.route)
    if not args.route.startswith("siwc-"):
        raise Error("SIWC route aliases must start with siwc- to select the OpenCode request profile")
    key = private_read(Path(args.workload_key_file), 4096).strip()
    if not key.startswith(b"sk-") or len(key) < 12 or any(c < 33 or c > 126 for c in key):
        raise Error("Expected a private LiteLLM workload key file")
    key_hash = hashlib.sha256(key).hexdigest()
    with owner.store.lock("runtime", timeout=0):
        models = owner.models(args.account)
        if args.model not in [model["slug"] for model in models]:
            raise Error("Selected model is not in this account's displayed catalog")
        with owner.store.lock():
            state = owner.store.read()
            record = state["accounts"][args.account]
            existing = state["routes"].get(args.route)
            route = {"account": args.account, "client_id": record["client_id"], "sub": record["sub"],
                     "model": args.model, "key_hashes": [key_hash], "consent_background": True,
                     "consent_connected_app": True}
            if existing and existing != route:
                raise Error("Route binding already exists; remove it before changing account, model or caller")
            state["routes"][args.route] = route
            owner.store.write(state)


def main(argv=None):
    parser = argparse.ArgumentParser(description="Aginctus SIWC owner; see docs/gateway-siwc.md")
    parser.add_argument("--state-dir", required=True)
    sub = parser.add_subparsers(dest="operation", required=True)
    for name in ("init", "login", "status", "models", "logout", "bind", "unbind", "render", "export", "receive"):
        cmd = sub.add_parser(name)
        if name in ("login", "models", "logout", "bind", "export"):
            cmd.add_argument("--account", required=True, type=valid_name)
        if name == "login":
            cmd.add_argument("--enable-plan", action="store_true")
        if name in ("bind", "unbind"):
            cmd.add_argument("--route", required=True, type=valid_name)
        if name == "bind":
            cmd.add_argument("--model", required=True)
            cmd.add_argument("--workload-key-file", required=True)
            cmd.add_argument("--consent-connected-app", action="store_true")
            cmd.add_argument("--consent-background", action="store_true")
        if name == "export":
            cmd.add_argument("--target-host-id", required=True)
            cmd.add_argument("--capsule", required=True)
        if name == "receive":
            cmd.add_argument("--capsule", required=True)
    args = parser.parse_args(argv)
    owner = Owner(Store(args.state_dir))
    if args.operation == "init":
        with owner.store.lock("runtime", timeout=0), owner.store.lock():
            state = owner.store.read()
            owner.store.write(state)
        print(state["host_id"])
    elif args.operation == "export":
        owner.export(args.account, args.target_host_id, args.capsule)
        print("Source session disabled. Transfer the private capsule securely, receive once, then remove both copies.")
    elif args.operation == "receive":
        owner.receive(args.capsule)
        print("SIWC handoff validated and persisted for this host. Remove the private capsule.")
    elif args.operation == "render":
        with owner.store.lock():
            state = owner.store.read()
        print(json.dumps({"model_list": [{"model_name": alias, "model_info": {"mode": "responses"},
              "litellm_params": {"model": "openai/" + route["model"], "api_base": RESOURCE,
                                 "api_key": "aginctus-siwc:" + alias}}
              for alias, route in state["routes"].items()],
              "litellm_settings": {"callbacks": ["litellm_siwc.proxy_handler_instance"],
                                   "turn_off_message_logging": True},
              "router_settings": {"num_retries": 0, "fallbacks": []}}, indent=2))
    elif args.operation == "login":
        login(owner, args.account, enable_plan=args.enable_plan)
    elif args.operation == "status":
        with owner.store.lock():
            state = owner.store.read()
            result = {label: {"client_id": record["client_id"], "subject": record.get("sub"),
                              "state": ("recovery_required" if record.get("refresh_pending") else
                                        "signed_out" if not record.get("access_token") else
                                        "plan_disabled" if DIRECT not in record.get("scopes", []) else
                                        "expired" if record["expires_at"] <= time.time() else "configured_not_remotely_verified")}
                      for label, record in state["accounts"].items()}
        print(json.dumps(result))
    elif args.operation == "models":
        with owner.store.lock("runtime", timeout=0):
            print(json.dumps(owner.models(args.account)))
    elif args.operation == "logout":
        confirmed = owner.logout(args.account)
        print("Local tokens cleared; registration retained. " + ("Remote revocation confirmed." if confirmed else
              "Remote revocation NOT confirmed; disconnect Aginctus in ChatGPT Settings."))
        return 0 if confirmed else 1
    elif args.operation == "bind":
        configure(owner, args)
        print("SIWC route bound to the selected registration, model and workload key.")
    else:
        with owner.store.lock("runtime", timeout=0), owner.store.lock():
            state = owner.store.read()
            state["routes"].pop(args.route, None)
            owner.store.write(state)
        print("SIWC route binding removed.")
    return 0


def entrypoint():
    try:
        return main()
    except (Error, OSError, KeyboardInterrupt):
        # Never format arbitrary exceptions: URLs, token replies and decoded JWT
        # payloads must not reach terminal output or upstream exception loggers.
        error = sys.exception()
        print(str(error) if isinstance(error, Error) else "SIWC operation interrupted or private state unavailable",
              file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(entrypoint())
