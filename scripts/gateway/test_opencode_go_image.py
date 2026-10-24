"""Offline packaging checks for the opt-in, non-default LiteLLM image."""
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
CONTEXT = ROOT / "deploy" / "litellm-go"


class GoImagePackagingTests(unittest.TestCase):
    def test_hook_files_and_config_are_opt_in_and_secret_free(self):
        docker = (CONTEXT / "Dockerfile").read_text(encoding="utf-8")
        config = (CONTEXT / "config.yaml").read_text(encoding="utf-8")
        self.assertIn("ARG LITELLM_BASE=ghcr.io/berriai/litellm:v1.103.0-stable", docker)
        self.assertIn("FROM ${LITELLM_BASE}", docker)
        self.assertIn("COPY --chown=0:0 --chmod=0444 scripts/gateway/opencode_go_forwarding.py", docker)
        self.assertIn("COPY --chown=0:0 --chmod=0444 scripts/gateway/litellm_opencode_go_hook.py", docker)
        self.assertIn("ENV PYTHONPATH=/app/aginctus-go", docker)
        self.assertIn('"--config", "/app/aginctus-go/config.yaml"', docker)
        self.assertIn("model_list: []", config)
        self.assertIn("litellm_opencode_go_hook.proxy_handler_instance", config)
        self.assertNotIn("forward_client_headers_to_llm_api:", config)
        for marker in ("api_key:", "master_key:", "salt_key:", "postgres_password:"):
            self.assertNotIn(marker, config.lower())

    def test_default_gateway_template_still_uses_unmodified_image(self):
        template = (ROOT / "internal" / "orchestration" / "resources" / "gateway.jsonnet").read_text()
        self.assertIn("image: cfg.litellmImage", template)
        self.assertNotIn("aginctus-go/config.yaml", template)
        self.assertNotIn("litellm_opencode_go_hook", template)


if __name__ == "__main__":
    unittest.main()
