package gateway

import (
 "encoding/json"
 "fmt"
 "regexp"
 "strings"
)

// ChatGPTModelRegistration produces the model configuration accepted by
// LiteLLM's model-management API. It never reads or exposes OAuth credentials.
// Registration is separate from activation until a writable private credential
// volume and explicit enrollment have been validated.
func ChatGPTModelRegistration(alias, model string) ([]byte,error) {
 safe:=regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
 if !safe.MatchString(alias) {return nil,fmt.Errorf("invalid LiteLLM model alias")}
 if !strings.HasPrefix(model,"chatgpt/") || !safe.MatchString(strings.TrimPrefix(model,"chatgpt/")) {
  return nil,fmt.Errorf("expected a chatgpt/<model> provider model")
 }
 registration:=struct{
  Name string `json:"model_name"`
  Params map[string]string `json:"litellm_params"`
  Info map[string]string `json:"model_info"`
 }{alias,map[string]string{"model":model},map[string]string{"mode":"responses"}}
 return json.MarshalIndent(registration,"","  ")
}
