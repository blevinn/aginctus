package gateway

import (
	"encoding/json"
	"fmt"

	gatewayassets "github.com/blevinn/aginctus/scripts/gateway"
	"sigs.k8s.io/yaml"
)

const SIWCStatePath = credentialMountPath + "/siwc"
const siwcPythonPath = "/app/aginctus-siwc"

func (s *Spec) ConfigureSIWC(configuration string) error {
	if !selectiveGoImagePattern.MatchString(s.LiteLLMImage) || s.HasClientHeaderForwarding() {
		return fmt.Errorf("managed SIWC requires an immutable LiteLLM image and forbids broad client-header forwarding")
	}
	var value map[string]any
	if err := json.Unmarshal([]byte(configuration), &value); err != nil {
		return err
	}
	if s.SelectiveGoForwarding {
		var base struct {
			Settings struct {
				Callbacks []string `json:"callbacks"`
			} `json:"litellm_settings"`
		}
		if err := yaml.Unmarshal([]byte(gatewayassets.Files()["config.yaml"]), &base); err != nil {
			return err
		}
		settings := value["litellm_settings"].(map[string]any)
		callbacks := settings["callbacks"].([]any)
		for _, callback := range base.Settings.Callbacks {
			callbacks = append(callbacks, callback)
		}
		settings["callbacks"] = callbacks
	}
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	s.SIWCConfig = string(data)
	return nil
}

func (s Spec) SIWCConfigs() map[string]map[string]string {
	configs := s.SelectiveGoConfigs()
	if s.SIWCConfig == "" {
		return configs
	}
	for name, content := range gatewayassets.SIWCFiles() {
		configs["aginctus-siwc-"+name] = map[string]string{"content": composeLiteralContent(content)}
	}
	configs["aginctus-siwc-config.json"] = map[string]string{"content": composeLiteralContent(s.SIWCConfig)}
	return configs
}

func (s Spec) SIWCAttachments() []map[string]string {
	attachments := []map[string]string{}
	if s.SelectiveGoForwarding {
		attachments = s.SelectiveGoAttachments()
	}
	for _, name := range []string{"config.json", "siwc.py", "litellm_siwc.py", "siwc_admin.py"} {
		attachments = append(attachments, map[string]string{"source": "aginctus-siwc-" + name, "target": siwcPythonPath + "/" + name})
	}
	return attachments
}

func (s Spec) SIWCPythonPath() string {
	if s.SelectiveGoForwarding {
		return siwcPythonPath + ":" + gatewayassets.PythonPath
	}
	return siwcPythonPath
}
