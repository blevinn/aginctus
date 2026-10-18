"""Shared validators for the OpenCode gateway configuration.

The provision and launch scripts both consume the gateway endpoint,
model alias, and workload key. Each previously duplicated the
validation rules and the duplicates diverged three ways:

- provision accepted a password-only URL (``url.username`` truthiness);
  launch rejected any userinfo.
- provision required ``url.path != "/v1"`` exactly; launch accepted a
  trailing slash.
- launch rejected whitespace in the endpoint; provision did not, and
  ``urllib.parse.urlsplit`` silently strips ``\\n`` and ``\\t`` from
  the URL before the path comparison runs, so a newline-passing URL
  was accepted at provision and rejected at launch with the generic
  "invalid endpoint" message.

This module owns the contract. Provision and launch call the same
``validate_endpoint`` / ``validate_model`` / ``validate_key`` helpers,
which makes the discrepancy structurally impossible and lets the
tests pin the rules in one place.
"""
from urllib.parse import urlsplit


def validate_endpoint(endpoint):
    """Return the parsed endpoint URL or raise ValueError.

    The rules are deliberately conservative: the endpoint must be
    plain HTTP (the gateway runs inside the management network, never
    over a public TLS-terminating proxy), carry no userinfo, query,
    or fragment, and resolve to the canonical ``/v1`` path. A
    trailing slash is tolerated for ergonomics but every other
    whitespace character is rejected, including the ``\\n`` and
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

    A model alias is a single token; it must be non-empty, have no
    leading/trailing whitespace, and contain no ``/`` (the model
    catalog uses slash-separated namespaces, but a private alias is
    expected to stay private).
    """
    if not isinstance(model, str) or not model or model.strip() != model or "/" in model:
        raise ValueError("invalid model")
    return model


def validate_key(key):
    """Return ``key`` or raise ValueError.

    A workload key is a single token with no embedded newlines or
    carriage returns and no leading/trailing whitespace.
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