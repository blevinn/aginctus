# syntax=docker/dockerfile:1.7
# Explicitly selected alternative to the stock v0.1 gateway image.
# For production promotion, pass the verified upstream digest as LITELLM_BASE.
ARG LITELLM_BASE=ghcr.io/berriai/litellm:v1.103.0-stable
FROM ${LITELLM_BASE}

# No credentials or operator-owned gateway state are copied into this image.
# Keep the callback import path separate from LiteLLM's installed modules.
COPY --chown=0:0 --chmod=0444 scripts/gateway/opencode_go_forwarding.py /app/aginctus-go/
COPY --chown=0:0 --chmod=0444 scripts/gateway/litellm_opencode_go_hook.py /app/aginctus-go/
COPY --chown=0:0 --chmod=0444 deploy/litellm-go/config.yaml /app/aginctus-go/config.yaml
ENV PYTHONPATH=/app/aginctus-go

# Prove real installed LiteLLM's callback base and the policy are importable.
# A build that cannot import the hook fails rather than starting unprotected.
RUN /app/.venv/bin/python -c "from litellm_opencode_go_hook import proxy_handler_instance; assert callable(proxy_handler_instance.async_pre_call_hook)"

# Preserve the upstream entrypoint. This alternative image alone opts in to
# loading the callback; the original stock gateway image remains unchanged.
CMD ["--config", "/app/aginctus-go/config.yaml", "--port", "4000"]
