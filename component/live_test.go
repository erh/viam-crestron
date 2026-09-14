package component

import (
	"context"
	"os"
	"testing"
	"time"

	"go.viam.com/rdk/logging"

	"github.com/erh/viam-crestron/crestron"
)

// liveController builds a controller from env creds and registers it so the
// light/scene/discovery components can find it the way they do in production.
func liveController(t *testing.T) *controller {
	crestron.LoadEnvFile()
	if os.Getenv("CRESTRON_TOKEN") == "" {
		t.Skip("no CRESTRON_TOKEN; skipping live test")
	}
	ctrl := &controller{logger: logging.NewTestLogger(t), client: crestron.New(resolveHost(""), resolveToken(""))}
	if err := ctrl.client.Login(context.Background()); err != nil {
		t.Fatalf("login: %v", err)
	}
	controllerRegistry.Store(defaultController, ctrl)
	t.Cleanup(func() { controllerRegistry.Delete(defaultController) })
	return ctrl
}

func TestLiveDiscovery(t *testing.T) {
	liveController(t)
	d := &discoverySvc{logger: logging.NewTestLogger(t), controller: defaultController, includeScenes: true}
	cfgs, err := d.DiscoverResources(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var lights, scenes int
	names := map[string]bool{}
	for _, cfg := range cfgs {
		if names[cfg.Name] {
			t.Fatalf("duplicate discovered name %q", cfg.Name)
		}
		names[cfg.Name] = true
		switch cfg.Model {
		case LightModel:
			lights++
		case SceneModel:
			scenes++
		}
	}
	t.Logf("discovered %d lights + %d scenes, all unique", lights, scenes)
	if lights == 0 || scenes == 0 {
		t.Fatalf("expected both lights and scenes, got %d/%d", lights, scenes)
	}
}

func TestLiveSwitchPositions(t *testing.T) {
	ctrl := liveController(t)
	const id = 52927 // Bar Pantry Countertop (dimmable)
	orig, err := ctrl.client.Light(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}

	l := &light{logger: logging.NewTestLogger(t), controller: defaultController, id: id, steps: (&LightConfig{Dimmable: true}).steps()}

	n, labels, err := l.GetNumberOfPositions(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if n != 6 {
		t.Fatalf("dimmable positions = %d, want 6", n)
	}
	t.Logf("positions=%d labels=%v", n, labels)

	if err := l.SetPosition(context.Background(), 2, nil); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Second)
	pos, err := l.GetPosition(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("after SetPosition(2): GetPosition=%d", pos)
	if pos != 2 {
		t.Fatalf("GetPosition=%d, want 2", pos)
	}
	if err := ctrl.client.SetLevel(context.Background(), id, orig.Level, 0); err != nil {
		t.Fatalf("restore: %v", err)
	}
	t.Logf("restored level to %d", orig.Level)
}
