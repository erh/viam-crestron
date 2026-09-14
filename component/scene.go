package component

import (
	"context"
	"fmt"

	"go.viam.com/rdk/components/button"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

// SceneModel is erh:crestron:scene — a Crestron scene exposed as a button whose
// Push recalls the scene. It links to a controller for the shared connection.
var SceneModel = resource.NewModel("erh", "crestron", "scene")

func init() {
	resource.RegisterComponent(button.API, SceneModel, resource.Registration[button.Button, *SceneConfig]{
		Constructor: newScene,
	})
}

// SceneConfig configures one scene button.
type SceneConfig struct {
	Controller string `json:"controller"`
	// ID is the Crestron scene id.
	ID int `json:"id"`
}

// Validate requires an id and declares the controller as a dependency.
func (c *SceneConfig) Validate(path string) ([]string, []string, error) {
	if c.ID == 0 {
		return nil, nil, fmt.Errorf(`%s: "id" is required`, path)
	}
	return nil, nil, nil
}

type scene struct {
	resource.Named
	resource.TriviallyCloseable

	logger     logging.Logger
	controller string
	id         int
}

func newScene(ctx context.Context, deps resource.Dependencies, conf resource.Config, logger logging.Logger) (button.Button, error) {
	s := &scene{Named: conf.ResourceName().AsNamed(), logger: logger}
	if err := s.Reconfigure(ctx, deps, conf); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *scene) Reconfigure(_ context.Context, _ resource.Dependencies, conf resource.Config) error {
	cfg, err := resource.NativeConfig[*SceneConfig](conf)
	if err != nil {
		return err
	}
	s.controller = controllerName(cfg.Controller)
	s.id = cfg.ID
	return nil
}

func (s *scene) Push(ctx context.Context, _ map[string]interface{}) error {
	ctrl, err := getRegisteredController(s.controller)
	if err != nil {
		return err
	}
	return ctrl.Client().RecallScene(ctx, s.id)
}
