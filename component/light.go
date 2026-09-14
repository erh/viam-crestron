package component

import (
	"context"
	"fmt"

	"go.viam.com/rdk/components/switch"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"
)

// LightModel is erh:crestron:light — one Crestron light load exposed as a
// switch. On/off loads are two-position (Off/On); dimmable loads default to six
// positions at 0/20/40/60/80/100%.
var LightModel = resource.NewModel("erh", "crestron", "light")

func init() {
	resource.RegisterComponent(toggleswitch.API, LightModel, resource.Registration[toggleswitch.Switch, *LightConfig]{
		Constructor: newLight,
	})
}

// LightConfig configures one light switch. It links to a controller component
// (default "crestron-main") for the shared connection.
type LightConfig struct {
	// Controller is the name of the erh:crestron:controller component to use.
	Controller string `json:"controller"`
	// ID is the Crestron load id.
	ID int `json:"id"`
	// Dimmable selects the default position set (six steps vs Off/On).
	Dimmable bool `json:"dimmable"`
	// Steps optionally overrides the percentage for each switch position.
	Steps []int `json:"steps,omitempty"`
	// Fade is the transition time in tenths of a second (0 = instant).
	Fade int `json:"fade,omitempty"`
}

// Validate requires an id and declares the controller as a dependency.
func (c *LightConfig) Validate(path string) ([]string, []string, error) {
	if c.ID == 0 {
		return nil, nil, fmt.Errorf(`%s: "id" is required`, path)
	}
	return nil, nil, nil
}

func (c *LightConfig) steps() []int {
	if len(c.Steps) > 0 {
		return c.Steps
	}
	if c.Dimmable {
		return []int{0, 20, 40, 60, 80, 100}
	}
	return []int{0, 100}
}

type light struct {
	resource.Named
	resource.TriviallyCloseable

	logger     logging.Logger
	controller string
	id         int
	steps      []int
	fade       int
}

func newLight(ctx context.Context, deps resource.Dependencies, conf resource.Config, logger logging.Logger) (toggleswitch.Switch, error) {
	l := &light{Named: conf.ResourceName().AsNamed(), logger: logger}
	if err := l.Reconfigure(ctx, deps, conf); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *light) Reconfigure(_ context.Context, _ resource.Dependencies, conf resource.Config) error {
	cfg, err := resource.NativeConfig[*LightConfig](conf)
	if err != nil {
		return err
	}
	l.controller = controllerName(cfg.Controller)
	l.id = cfg.ID
	l.steps = cfg.steps()
	l.fade = cfg.Fade
	return nil
}

func (l *light) SetPosition(ctx context.Context, position uint32, _ map[string]interface{}) error {
	if int(position) >= len(l.steps) {
		return fmt.Errorf("position %d out of range (0-%d)", position, len(l.steps)-1)
	}
	ctrl, err := getRegisteredController(l.controller)
	if err != nil {
		return err
	}
	if err := ctrl.Client().SetPercent(ctx, l.id, l.steps[position], l.fade); err != nil {
		return err
	}
	ctrl.Invalidate()
	return nil
}

func (l *light) GetPosition(ctx context.Context, _ map[string]interface{}) (uint32, error) {
	ctrl, err := getRegisteredController(l.controller)
	if err != nil {
		return 0, err
	}
	lt, err := ctrl.LightState(ctx, l.id)
	if err != nil {
		return 0, err
	}
	pct := lt.Percent()
	best, bestDiff := 0, 1<<30
	for i, s := range l.steps {
		d := s - pct
		if d < 0 {
			d = -d
		}
		if d < bestDiff {
			best, bestDiff = i, d
		}
	}
	return uint32(best), nil
}

func (l *light) GetNumberOfPositions(_ context.Context, _ map[string]interface{}) (uint32, []string, error) {
	labels := make([]string, len(l.steps))
	for i, s := range l.steps {
		switch {
		case s == 0:
			labels[i] = "Off"
		case s == 100 && len(l.steps) == 2:
			labels[i] = "On"
		default:
			labels[i] = fmt.Sprintf("%d%%", s)
		}
	}
	return uint32(len(l.steps)), labels, nil
}
