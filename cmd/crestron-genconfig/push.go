package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"go.viam.com/rdk/app"
	"go.viam.com/rdk/logging"
)

// componentModels are the component models this util owns; existing entries with
// these models are replaced on push so re-running updates in place.
var componentModels = map[string]bool{
	"erh:crestron:controller": true,
	"erh:crestron:light":      true,
	"erh:crestron:scene":      true,
}

var serviceModels = map[string]bool{
	"erh:crestron:discovery": true,
}

// pushToMachine merges the controller, light/scene components, and discovery
// service into a machine part's config via the Viam app API. It is idempotent:
// it replaces the crestron resources it owns and leaves everything else alone.
// The module itself is deployed separately with `viam module reload-local`.
func pushToMachine(ctx context.Context, partID, apiKeyID, apiKey string, components, services []map[string]interface{}, logger logging.Logger) error {
	vc, err := app.CreateViamClientWithAPIKey(ctx, app.Options{}, apiKey, apiKeyID, logger)
	if err != nil {
		return fmt.Errorf("connecting to viam app: %w", err)
	}
	defer vc.Close()
	ac := vc.AppClient()

	part, _, err := ac.GetRobotPart(ctx, partID)
	if err != nil {
		return fmt.Errorf("getting robot part %q: %w", partID, err)
	}
	conf := part.RobotConfig
	if conf == nil {
		conf = map[string]interface{}{}
	}

	conf["components"] = replaceByModel(conf["components"], componentModels, toAny(components))
	conf["services"] = replaceByModel(conf["services"], serviceModels, toAny(services))

	warnIfNoModule(conf)

	if _, err := ac.UpdateRobotPart(ctx, partID, part.Name, conf); err != nil {
		return fmt.Errorf("updating robot part: %w", err)
	}
	fmt.Fprintf(os.Stderr, "pushed to %q: %d components + %d services\n", part.Name, len(components), len(services))
	return nil
}

func toAny(ms []map[string]interface{}) []interface{} {
	out := make([]interface{}, len(ms))
	for i, m := range ms {
		out[i] = m
	}
	return out
}

// replaceByModel drops entries from arr whose "model" is in models, then appends
// adds. Non-matching entries are preserved.
func replaceByModel(arr interface{}, models map[string]bool, adds []interface{}) []interface{} {
	var kept []interface{}
	if existing, ok := arr.([]interface{}); ok {
		for _, e := range existing {
			if m, ok := e.(map[string]interface{}); ok {
				if model, _ := m["model"].(string); models[model] {
					continue
				}
			}
			kept = append(kept, e)
		}
	}
	return append(kept, adds...)
}

// warnIfNoModule notes when no module in the config appears to provide the
// erh:crestron models (deploy it with `viam module reload-local`).
func warnIfNoModule(conf map[string]interface{}) {
	if mods, ok := conf["modules"].([]interface{}); ok {
		for _, m := range mods {
			if b, _ := json.Marshal(m); len(b) > 0 && containsCrestron(string(b)) {
				return
			}
		}
	}
	fmt.Fprintln(os.Stderr, "warning: no crestron module found in the machine config; "+
		"deploy it with `viam module reload-local --local` so the erh:crestron models resolve.")
}

func containsCrestron(s string) bool {
	for i := 0; i+8 <= len(s); i++ {
		if s[i:i+8] == "crestron" {
			return true
		}
	}
	return false
}
