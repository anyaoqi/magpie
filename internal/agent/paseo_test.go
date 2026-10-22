package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/gateway"
)

func TestPaseoConnectSyncAndDisconnect(t *testing.T) {
	home := syncHome(t)
	paseoHome := filepath.Join(home, "paseo-home")
	t.Setenv("PASEO_HOME", paseoHome)
	path := filepath.Join(paseoHome, "config.json")
	writeFile(t, path, `{"theme":"dark","agents":{"providers":{"other":{"label":"Keep me"}}}}`)

	reloads := 0
	oldReload := requestPaseoReload
	requestPaseoReload = func() { reloads++ }
	t.Cleanup(func() { requestPaseoReload = oldReload })
	a := paseo(home)
	if got, err := a.Join(); err != nil || !got {
		t.Fatalf("Join() = %v, %v; want true, nil", got, err)
	}
	if reloads != 1 {
		t.Fatalf("reloads after connect = %d, want 1", reloads)
	}

	config := readPaseoConfig(t, path)
	if config["theme"] != "dark" {
		t.Errorf("unrelated config was lost: %#v", config["theme"])
	}
	providers := config["agents"].(map[string]any)["providers"].(map[string]any)
	if providers["other"].(map[string]any)["label"] != "Keep me" {
		t.Errorf("unrelated provider was lost: %#v", providers["other"])
	}
	managed := providers[paseoProviderID].(map[string]any)
	if managed["extends"] != "codex" || managed["description"] != paseoProviderMarker {
		t.Errorf("unexpected managed provider: %#v", managed)
	}
	env := managed["env"].(map[string]any)
	if env["OPENAI_BASE_URL"] != gatewayURLForTest(t) || env["OPENAI_API_KEY"] == "" {
		t.Errorf("unexpected gateway env: %#v", env)
	}
	models := managed["models"].([]any)
	if len(models) != 1 || models[0].(map[string]any)["id"] != "relay/glm-4.6" || models[0].(map[string]any)["isDefault"] != true {
		t.Errorf("unexpected models: %#v", models)
	}

	if err := a.Sync(); err != nil {
		t.Fatal(err)
	}
	if reloads != 1 {
		t.Errorf("unchanged sync triggered reload, count = %d", reloads)
	}
	if err := a.Unwire(); err != nil {
		t.Fatal(err)
	}
	if reloads != 2 {
		t.Errorf("reloads after disconnect = %d, want 2", reloads)
	}
	config = readPaseoConfig(t, path)
	if config["theme"] != "dark" || config["agents"].(map[string]any)["providers"].(map[string]any)["other"] == nil {
		t.Errorf("disconnect changed unrelated settings: %#v", config)
	}
	if _, ok := config["agents"].(map[string]any)["providers"].(map[string]any)[paseoProviderID]; ok {
		t.Error("managed provider remains after disconnect")
	}
}

func TestPaseoDoesNotOverwriteUserProvider(t *testing.T) {
	home := syncHome(t)
	path := filepath.Join(home, ".paseo", "config.json")
	writeFile(t, path, `{"agents":{"providers":{"magpie-gateway":{"label":"User config"}}}}`)
	a := paseo(home)
	if _, err := a.Join(); err == nil {
		t.Fatal("Join() overwrote a provider not managed by Magpie")
	}
	if got := readFile(path); got != `{"agents":{"providers":{"magpie-gateway":{"label":"User config"}}}}` {
		t.Errorf("config changed after conflict: %s", got)
	}
}

func readPaseoConfig(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var config map[string]any
	if err := json.Unmarshal(b, &config); err != nil {
		t.Fatal(err)
	}
	return config
}

func gatewayURLForTest(t *testing.T) string {
	t.Helper()
	return gateway.URL()
}
