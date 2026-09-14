package component

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"go.viam.com/rdk/components/button"
	"go.viam.com/rdk/components/switch"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/discovery"
	"go.viam.com/rdk/utils"

	"github.com/erh/viam-crestron/crestron"
)

// DiscoveryModel is erh:crestron:discovery — a discovery service that returns
// one switch per light and one button per scene, each linked to the controller.
var DiscoveryModel = resource.NewModel("erh", "crestron", "discovery")

func init() {
	resource.RegisterService(discovery.API, DiscoveryModel, resource.Registration[discovery.Service, *DiscoveryConfig]{
		Constructor: newDiscovery,
	})
}

// DiscoveryConfig configures the discovery service. It links to a controller for
// the connection, and stamps that controller name into every discovered config.
type DiscoveryConfig struct {
	Controller string `json:"controller"`
	// IncludeScenes controls whether scene buttons are discovered (default true).
	IncludeScenes *bool `json:"include_scenes,omitempty"`
}

// Validate declares the controller as a dependency.
func (c *DiscoveryConfig) Validate(path string) ([]string, []string, error) {
	return nil, nil, nil
}

type discoverySvc struct {
	resource.Named
	resource.TriviallyCloseable

	logger        logging.Logger
	controller    string
	includeScenes bool
}

func newDiscovery(ctx context.Context, deps resource.Dependencies, conf resource.Config, logger logging.Logger) (discovery.Service, error) {
	d := &discoverySvc{Named: conf.ResourceName().AsNamed(), logger: logger}
	if err := d.Reconfigure(ctx, deps, conf); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *discoverySvc) Reconfigure(_ context.Context, _ resource.Dependencies, conf resource.Config) error {
	cfg, err := resource.NativeConfig[*DiscoveryConfig](conf)
	if err != nil {
		return err
	}
	d.controller = controllerName(cfg.Controller)
	d.includeScenes = cfg.IncludeScenes == nil || *cfg.IncludeScenes
	return nil
}

func (d *discoverySvc) DiscoverResources(ctx context.Context, _ map[string]any) ([]resource.Config, error) {
	ctrl, err := getRegisteredController(d.controller)
	if err != nil {
		return nil, err
	}
	return DiscoverConfigs(ctx, ctrl.Client(), d.controller, d.includeScenes)
}

// Discovered pairs a resource config with the folder (room) it belongs in. The
// folder is surfaced separately because it is written as a top-level "ui_folder"
// field, which resource.Config does not model.
type Discovered struct {
	Config resource.Config
	Folder string
}

// DiscoverConfigs returns just the configs (used by the discovery service, which
// cannot carry folders through its typed return).
func DiscoverConfigs(ctx context.Context, client *crestron.Client, controller string, includeScenes bool) ([]resource.Config, error) {
	all, err := DiscoverAll(ctx, client, controller, includeScenes, nil)
	if err != nil {
		return nil, err
	}
	out := make([]resource.Config, len(all))
	for i, d := range all {
		out[i] = d.Config
	}
	return out, nil
}

// DiscoverAll returns one switch per light and one button per scene, each with
// the room it belongs to as its folder. It backs the crestron-genconfig util.
func DiscoverAll(ctx context.Context, client *crestron.Client, controller string, includeScenes bool, renames map[string]string) ([]Discovered, error) {
	controller = controllerName(controller)
	lights, err := client.Lights(ctx)
	if err != nil {
		return nil, err
	}
	rooms, err := client.Rooms(ctx)
	if err != nil {
		return nil, err
	}
	roomName := make(map[int]string, len(rooms))
	for _, r := range rooms {
		name := r.Name
		if alt, ok := renames[name]; ok {
			name = alt
		}
		roomName[r.ID] = name
	}

	used := map[string]bool{}
	var out []Discovered

	for _, lt := range lights {
		room := folderName(roomName[lt.RoomID])
		name := uniqueName(used, roomName[lt.RoomID], lt.Name, lt.ID)
		out = append(out, Discovered{Folder: room, Config: resource.Config{
			Name:  name,
			API:   toggleswitch.API,
			Model: LightModel,
			Attributes: utils.AttributeMap{
				"controller": controller,
				"id":         lt.ID,
				"dimmable":   lt.Dimmable(),
			},
		}})
	}

	if includeScenes {
		scenes, err := client.Scenes(ctx)
		if err != nil {
			return nil, err
		}
		for _, s := range scenes {
			room := folderName(roomName[s.RoomID])
			name := uniqueName(used, roomName[s.RoomID], "scene "+s.Name, s.ID)
			out = append(out, Discovered{Folder: room, Config: resource.Config{
				Name:  name,
				API:   button.API,
				Model: SceneModel,
				Attributes: utils.AttributeMap{
					"controller": controller,
					"id":         s.ID,
				},
			}})
		}
	}

	return out, nil
}

var nameSanitizer = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = nameSanitizer.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// folderName maps a room name to a UI folder, defaulting unassigned loads to
// their own folder.
func folderName(room string) string {
	if strings.TrimSpace(room) == "" {
		return "Unassigned"
	}
	return room
}

func uniqueName(used map[string]bool, room, name string, id int) string {
	base := strings.Trim(slug(room)+"-"+slug(name), "-")
	if base == "" {
		base = fmt.Sprintf("crestron-%d", id)
	}
	candidate := base
	if used[candidate] {
		candidate = fmt.Sprintf("%s-%d", base, id)
	}
	used[candidate] = true
	return candidate
}
