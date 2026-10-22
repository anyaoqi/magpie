package agent

// Paseo keeps custom agent providers in $PASEO_HOME/config.json (normally
// ~/.paseo/config.json). A custom Codex provider can point at an OpenAI
// Responses API endpoint, so Magpie's gateway can be offered as a provider
// without changing Paseo itself.

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yetone/magpie/internal/appdir"
	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/gateway"
	"github.com/yetone/magpie/internal/proc"
)

const paseoProviderID = "magpie-gateway"
const paseoProviderMarker = "Managed by Magpie (Paseo integration)"

var paseoNotice = newNotice("Magpie asks Paseo to reload its provider config after changes. If reload fails, reopen Paseo. Running agent sessions need a restart to use the change.")

// requestPaseoReload is replaceable in tests so they never launch the user's
// installed Paseo process.
var requestPaseoReload = reloadPaseo

type paseoModel struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	IsDefault bool   `json:"isDefault,omitempty"`
}

func paseo(home string) *Agent {
	paseoHome := appdir.Getenv("PASEO_HOME")
	if paseoHome == "" {
		paseoHome = filepath.Join(home, ".paseo")
	}
	path := filepath.Join(paseoHome, "config.json")
	providerPath := "agents.providers." + paseoProviderID
	wired := func() bool {
		var description string
		raw, ok := edit.GetJSONRaw(path, providerPath+".description")
		if !ok || json.Unmarshal([]byte(raw), &description) != nil {
			return false
		}
		return description == paseoProviderMarker
	}
	write := func(on bool) error {
		before, hadBefore := edit.GetJSONRaw(path, providerPath)
		var err error
		if on {
			if _, ok := edit.GetJSONRaw(path, providerPath); ok && !wired() {
				return fmt.Errorf("Paseo already has a custom provider named %q; rename it before connecting Magpie", paseoProviderID)
			}
			if wired() {
				err = syncJSON(path, providerPath, theirsKept(path, providerPath, func() any { return paseoProvider(gateway.URL()) }, "models"))
			} else {
				err = edit.SetJSON(path, edit.KV{Path: providerPath, Value: paseoProvider(gateway.URL())})
			}
		} else if hadBefore && wired() {
			err = edit.DelJSON(path, providerPath)
		}
		if err != nil {
			return err
		}
		after, hasAfter := edit.GetJSONRaw(path, providerPath)
		if hadBefore != hasAfter || before != after {
			requestPaseoReload()
		}
		return nil
	}
	return &Agent{
		ID: "paseo", Name: "Paseo", Icon: "paseo-color", Path: path,
		Notice: func() string { return paseoNotice.String() },
		detect: func() bool {
			if _, err := os.Stat(path); err == nil {
				return true
			}
			return binAt("paseo") != ""
		},
		ListsModels: true,
		Sync: func() error {
			if !wired() {
				return nil
			}
			return write(true)
		},
		// Paseo has no model field for Magpie to point at. Connecting it
		// means adding the custom provider to Paseo's config, so use Join to
		// apply that provider and report the connection without choosing a
		// Magpie model as Paseo's active model.
		Join: func() (bool, error) {
			return true, write(true)
		},
		Joined: func() bool { return wired() },
		Unwire: func() error { return write(false) },
		Fields: []Field{{
			Key: "provider", Label: "provider",
			Get: func() string {
				if wired() {
					return paseoProviderID
				}
				return ""
			},
			Set: func(v string) error { return write(v != "") },
			Options: func(map[string]string) []Option {
				return []Option{{Value: paseoProviderID, Label: "magpie", Icon: "magpie", Note: "Magpie models in Paseo's Codex provider list"}}
			},
		}},
	}
}

// Paseo reads provider config when launching agents. Ask its daemon to reload
// after Magpie changes the managed entry, so the user doesn't have to restart
// the Paseo app. Existing agent sessions keep the config they launched with.
func reloadPaseo() {
	bin := binAt("paseo")
	if bin == "" {
		log.Printf("paseo reload skipped: paseo CLI not found; reopen Paseo to load updated config")
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		out, err := proc.CommandContext(ctx, bin, "reload").CombinedOutput()
		if err != nil {
			log.Printf("paseo reload failed: %v: %s", err, strings.TrimSpace(string(out)))
		}
	}()
}

func paseoProvider(gw string) map[string]any {
	models := magpieModels("paseo")
	entries := make([]paseoModel, 0, len(models))
	for i, model := range models {
		entries = append(entries, paseoModel{ID: model.ID, Label: model.Name, IsDefault: i == 0})
	}
	if entries == nil {
		entries = []paseoModel{}
	}
	return map[string]any{
		"extends":     "codex",
		"label":       "Magpie",
		"description": paseoProviderMarker,
		"env": map[string]string{
			"OPENAI_API_KEY":  agentKeyAt("paseo", gw),
			"OPENAI_BASE_URL": gw,
		},
		"models": entries,
	}
}
