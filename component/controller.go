package component

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.viam.com/rdk/components/generic"
	"go.viam.com/rdk/logging"
	"go.viam.com/rdk/resource"

	"github.com/erh/viam-crestron/crestron"
)

// ControllerModel is erh:crestron:controller — the single connection component
// (conventionally named "crestron-main"). Every light and scene depends on it
// and shares its one *crestron.Client (and thus its session and re-login
// handling), rather than each opening its own connection.
var ControllerModel = resource.NewModel("erh", "crestron", "controller")

func init() {
	resource.RegisterComponent(generic.API, ControllerModel, resource.Registration[resource.Resource, *ControllerConfig]{
		Constructor: newController,
	})
}

// Controller is implemented by the controller component and looked up by the
// light and scene components to obtain the shared client and cached state.
type Controller interface {
	resource.Resource
	// Client is the shared API client, used for writes and scene recalls.
	Client() *crestron.Client
	// LightState returns one load from an in-memory snapshot that a background
	// poller keeps fresh. It never blocks on the processor, so many lights
	// reading their position at once cost nothing on the wire.
	LightState(ctx context.Context, id int) (crestron.Light, error)
	// Invalidate asks the poller to refresh soon (call after a write).
	Invalidate()
}

// controllerRegistry holds live controllers by name so light and scene
// components can share one without an RDK dependency edge (which, with hundreds
// of resources pointing at one controller, makes reconfiguration a huge
// cascade). Lookups are per-call, so components always use the current one.
var controllerRegistry sync.Map // name -> *controller

// getRegisteredController returns the named controller if it is up.
func getRegisteredController(name string) (Controller, error) {
	v, ok := controllerRegistry.Load(controllerName(name))
	if !ok {
		return nil, fmt.Errorf("crestron controller %q not ready", controllerName(name))
	}
	return v.(Controller), nil
}

// pollInterval is how often the controller refreshes its snapshot of all lights.
// One request on this interval covers every light, regardless of how many are
// polling their position.
const pollInterval = 4 * time.Second

// ControllerConfig configures the shared connection. Token is the Web API token
// used for all REST control; Host defaults to CRESTRON_HOST then 192.168.3.2,
// and Token falls back to CRESTRON_TOKEN (env or ~/.crestron.env). User and
// Password are stored for SSH/console features and are not used for REST.
type ControllerConfig struct {
	Host     string `json:"host"`
	Token    string `json:"token"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
}

// Validate requires a resolvable token (config or env) and declares no deps.
func (c *ControllerConfig) Validate(path string) ([]string, []string, error) {
	if resolveToken(c.Token) == "" {
		return nil, nil, fmt.Errorf(`%s: no token: set "token" or the CRESTRON_TOKEN env var`, path)
	}
	return nil, nil, nil
}

type controller struct {
	resource.Named

	logger logging.Logger
	client *crestron.Client

	mu       sync.RWMutex
	snapshot map[int]crestron.Light

	cancel  context.CancelFunc
	refresh chan struct{}
}

func newController(ctx context.Context, _ resource.Dependencies, conf resource.Config, logger logging.Logger) (resource.Resource, error) {
	c := &controller{Named: conf.ResourceName().AsNamed(), logger: logger}
	if err := c.Reconfigure(ctx, nil, conf); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *controller) Reconfigure(ctx context.Context, _ resource.Dependencies, conf resource.Config) error {
	cfg, err := resource.NativeConfig[*ControllerConfig](conf)
	if err != nil {
		return err
	}

	// Stop any previous poller before swapping the client.
	if c.cancel != nil {
		c.cancel()
	}

	c.client = crestron.New(resolveHost(cfg.Host), resolveToken(cfg.Token))
	c.mu.Lock()
	c.snapshot = nil
	c.mu.Unlock()

	// Validate eagerly, but don't fail construction if the processor is briefly
	// unreachable (e.g. rebooting) — the poller retries and the client logs in
	// lazily, so the module stays up and recovers on its own.
	if err := c.client.Login(ctx); err != nil {
		c.logger.Warnw("crestron login failed at startup; will retry", "host", resolveHost(cfg.Host), "error", err)
	} else {
		c.logger.Infow("crestron controller connected", "host", resolveHost(cfg.Host))
	}

	pollCtx, cancel := context.WithCancel(context.Background())
	c.cancel = cancel
	c.refresh = make(chan struct{}, 1)
	go c.poll(pollCtx)

	controllerRegistry.Store(c.Name().Name, c)
	return nil
}

// poll refreshes the snapshot on an interval and whenever a write asks for it.
func (c *controller) poll(ctx context.Context) {
	c.refreshOnce(ctx)
	t := time.NewTicker(pollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-c.refresh:
		}
		c.refreshOnce(ctx)
	}
}

func (c *controller) refreshOnce(ctx context.Context) {
	lights, err := c.client.Lights(ctx)
	if err != nil {
		c.logger.Debugw("light snapshot refresh failed", "error", err)
		return
	}
	m := make(map[int]crestron.Light, len(lights))
	for _, l := range lights {
		m[l.ID] = l
	}
	c.mu.Lock()
	c.snapshot = m
	c.mu.Unlock()
}

// Client returns the shared API client for dependents.
func (c *controller) Client() *crestron.Client { return c.client }

// LightState returns one load from the snapshot without ever touching the
// processor. Before the first poll completes it falls back to a single direct
// read so early callers still get an answer.
func (c *controller) LightState(ctx context.Context, id int) (crestron.Light, error) {
	c.mu.RLock()
	snap := c.snapshot
	c.mu.RUnlock()

	if snap == nil {
		return c.client.Light(ctx, id)
	}
	lt, ok := snap[id]
	if !ok {
		return crestron.Light{}, fmt.Errorf("no light with id %d", id)
	}
	return lt, nil
}

// Invalidate asks the poller to refresh soon (non-blocking).
func (c *controller) Invalidate() {
	select {
	case c.refresh <- struct{}{}:
	default:
	}
}

// Close stops the background poller and unregisters the controller.
func (c *controller) Close(context.Context) error {
	controllerRegistry.Delete(c.Name().Name)
	if c.cancel != nil {
		c.cancel()
	}
	return nil
}

// DoCommand supports {"ping": true} to re-check the connection.
func (c *controller) DoCommand(ctx context.Context, cmd map[string]interface{}) (map[string]interface{}, error) {
	if _, ok := cmd["ping"]; ok {
		if err := c.client.Login(ctx); err != nil {
			return nil, err
		}
		return map[string]interface{}{"ok": true}, nil
	}
	return nil, fmt.Errorf("unknown command; supported: ping")
}

// defaultController is the conventional controller name.
const defaultController = "crestron-main"

func controllerName(name string) string {
	if name == "" {
		return defaultController
	}
	return name
}
