// Command crestron-genconfig connects to a Crestron Home processor and produces
// a Viam config for it: one erh:crestron:controller ("crestron-main") holding
// the shared connection, one switch per light, one button per scene (all linked
// to the controller), and an erh:crestron:discovery service. Every resource is
// placed in a "ui_folder" — lights and scenes in their room, the controller and
// service in a "Crestron" folder.
//
// It does NOT configure the module itself — deploy that with
// `viam module reload-local --local`, which adds the module entry.
//
//	crestron-genconfig                       # print the config JSON
//	crestron-genconfig -push -part <id> ...  # write it to a machine via the app API
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"

	"go.viam.com/rdk/components/generic"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/discovery"
	"go.viam.com/rdk/utils"

	"github.com/erh/viam-crestron/component"
	"github.com/erh/viam-crestron/crestron"
)

// crestronFolder groups the controller and discovery service together.
const crestronFolder = "Crestron"

func main() {
	crestron.LoadEnvFile()
	host := flag.String("host", os.Getenv("CRESTRON_HOST"), "processor address (default env, then 192.168.3.2)")
	token := flag.String("token", os.Getenv("CRESTRON_TOKEN"), "web API token (default env)")
	controllerName := flag.String("controller", "crestron-main", "name of the controller component")
	noScenes := flag.Bool("no-scenes", false, "omit scene buttons")
	push := flag.Bool("push", false, "write to a Viam machine via the app API instead of printing")
	apiKeyID := flag.String("api-key-id", os.Getenv("VIAM_API_KEY_ID"), "Viam API key ID (for -push)")
	apiKey := flag.String("api-key", os.Getenv("VIAM_API_KEY"), "Viam API key (for -push)")
	part := flag.String("part", os.Getenv("VIAM_PART_ID"), "Viam machine part ID (for -push)")
	flag.Parse()

	if *token == "" {
		fmt.Fprintln(os.Stderr, "error: no token; set CRESTRON_TOKEN (env or ~/.crestron.env) or pass -token")
		os.Exit(1)
	}
	h := *host
	if h == "" {
		h = "192.168.3.2"
	}
	client := crestron.New(h, *token)

	// Clean up awkward Crestron room names for folders and component names.
	renames := map[string]string{
		"Dining Room (New)":      "Dining Room",
		"Dining Room":            "Dining Room 2",
		"Stairwell  - East Side": "Stairwell - East Side",
	}
	discovered, err := component.DiscoverAll(context.Background(), client, *controllerName, !*noScenes, renames)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	sort.Slice(discovered, func(i, j int) bool { return discovered[i].Config.Name < discovered[j].Config.Name })

	// Controller holds only the host; the token is resolved from the module's
	// environment (CRESTRON_ENV) so no secret lands in the machine config.
	controllerCfg := resource.Config{
		Name:       *controllerName,
		API:        generic.API,
		Model:      component.ControllerModel,
		Attributes: utils.AttributeMap{"host": h},
	}
	components := []map[string]interface{}{withFolder(mustMap(controllerCfg), crestronFolder)}
	for _, d := range discovered {
		components = append(components, withFolder(mustMap(d.Config), d.Folder))
	}

	discoveryCfg := resource.Config{
		Name:       "crestron-discovery",
		API:        discovery.API,
		Model:      component.DiscoveryModel,
		DependsOn:  []string{*controllerName},
		Attributes: utils.AttributeMap{"controller": *controllerName},
	}
	services := []map[string]interface{}{withFolder(mustMap(discoveryCfg), crestronFolder)}

	if *push {
		if *apiKeyID == "" || *apiKey == "" || *part == "" {
			fmt.Fprintln(os.Stderr, "error: -push needs -api-key-id, -api-key, and -part (or VIAM_API_KEY_ID/VIAM_API_KEY/VIAM_PART_ID)")
			os.Exit(1)
		}
		if err := pushToMachine(context.Background(), *part, *apiKeyID, *apiKey, components, services, logging.NewLogger("crestron-genconfig")); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}

	out := map[string]interface{}{"components": components, "services": services}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "\n# %d components + 1 discovery service, foldered by room\n", len(components))
}

// mustMap converts a resource.Config to its JSON map form.
func mustMap(cfg resource.Config) map[string]interface{} {
	b, err := json.Marshal(cfg)
	if err != nil {
		panic(err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		panic(err)
	}
	return m
}

// withFolder stamps the "ui_folder" field that groups the resource in the app's
// Resources sidebar.
func withFolder(m map[string]interface{}, folder string) map[string]interface{} {
	m["ui_folder"] = map[string]interface{}{"name": folder}
	return m
}
