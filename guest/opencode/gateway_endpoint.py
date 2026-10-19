"""Shared validators for the OpenCode gateway configuration.

Provision and launch share the endpoint, model-alias, and workload-key
syntax contract through these helpers. Validation does not check network
isolation, endpoint reachability, model availability, or key authorization.
See ``docs/workloads/opencode.md`` for the guest runtime contract.
"""
from urllib.parse import urlsplit


def validate_endpoint(endpoint):
    """Return the parsed endpoint URL or raise ValueError.

    The rules are deliberately conservative: the endpoint must be
    plain HTTP (the gateway runs inside the management network, never
    over a public TLS-terminating proxy), carry no userinfo, query,
    or fragment, and have path ``/v1`` after removing trailing slashes.
    All whitespace is rejected before parsing, including the ``\\n`` and
    ``\\t`` that ``urlsplit`` silently strips.
    """
    if not isinstance(endpoint, str):
        raise ValueError("invalid endpoint")
    if any(c.isspace() for c in endpoint):
        raise ValueError("invalid endpoint")
    url = urlsplit(endpoint)
    if (
        url.scheme != "http"
        or not url.hostname
        or url.username is not None
        or url.password is not None
        or url.query
        or url.fragment
        or url.path.rstrip("/") != "/v1"
    ):
        raise ValueError("invalid endpoint")
    return url


def validate_model(model):
    """Return ``model`` or raise ValueError.

    A model alias must be non-empty, have no leading/trailing whitespace,
    and contain no ``/``. Internal whitespace is not checked here.
    """
    if not isinstance(model, str) or not model or model.strip() != model or "/" in model:
        raise ValueError("invalid model")
    return model


def validate_key(key):
    """Return ``key`` or raise ValueError.

    A workload key must be non-empty, with no embedded newlines or
    carriage returns and no leading/trailing whitespace. Other internal
    whitespace is not checked here; validity at LiteLLM is separate.
    """
    if (
        not isinstance(key, str)
        or not key
        or key.strip() != key
        or "\n" in key
        or "\r" in key
    ):
        raise ValueError("invalid key")
    return key
