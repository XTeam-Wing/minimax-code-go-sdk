package desktop

import (
	"encoding/json"
	"os"
	"testing"
)

func TestInventoryFileMatchesCatalog(t *testing.T) {
	body, err := os.ReadFile("../internal/inventory/desktop_service.json")
	if err != nil {
		t.Fatal(err)
	}
	var inv struct {
		Artifact struct {
			Version       string `json:"version"`
			TarballSHA256 string `json:"tarball_sha256"`
		} `json:"artifact"`
		Denominators struct {
			DesktopServiceMethods int `json:"desktop_service_methods"`
			SSE                   int `json:"sse"`
			GatewayOverride       int `json:"gateway_override"`
			WholeBody             int `json:"whole_body"`
		} `json:"denominators"`
		Methods []struct {
			MethodName      string `json:"method_name"`
			HTTPMethod      string `json:"http_method"`
			Path            string `json:"path"`
			Transport       string `json:"transport"`
			GatewayOverride bool   `json:"gateway_override"`
		} `json:"methods"`
	}
	if err := json.Unmarshal(body, &inv); err != nil {
		t.Fatal(err)
	}
	if inv.Artifact.Version != BaselineVersion || inv.Artifact.TarballSHA256 != "f497f02bac0f8fc8ad3791d59e8827208684ab466bec94cd7702594d19f5735a" {
		t.Fatalf("artifact %+v", inv.Artifact)
	}
	if inv.Denominators.DesktopServiceMethods != 470 {
		t.Fatalf("denominator %d", inv.Denominators.DesktopServiceMethods)
	}
	cat := MethodCatalog()
	if len(inv.Methods) != len(cat) {
		t.Fatalf("inventory %d catalog %d", len(inv.Methods), len(cat))
	}
	byName := map[string]MethodInfo{}
	for _, item := range cat {
		byName[item.Name] = item
	}
	for _, method := range inv.Methods {
		item, ok := byName[method.MethodName]
		if !ok {
			t.Fatalf("missing %s", method.MethodName)
		}
		if item.HTTPMethod != method.HTTPMethod || item.Path != method.Path || item.Transport != method.Transport || item.GatewayOverride != method.GatewayOverride {
			t.Fatalf("mismatch %+v vs %+v", method, item)
		}
	}
}
