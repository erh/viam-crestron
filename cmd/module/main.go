// Command module runs the Crestron Viam module, exposing:
//
//	erh:crestron:controller (generic)   the shared connection (crestron-main)
//	erh:crestron:light      (switch)    one light load, links to the controller
//	erh:crestron:scene      (button)    recall one scene, links to the controller
//	erh:crestron:discovery  (service)   discover all lights and scenes
package main

import (
	"go.viam.com/rdk/components/button"
	"go.viam.com/rdk/components/generic"
	"go.viam.com/rdk/components/switch"
	"go.viam.com/rdk/module"
	"go.viam.com/rdk/resource"
	"go.viam.com/rdk/services/discovery"

	"github.com/erh/viam-crestron/component"
)

func main() {
	module.ModularMain(
		resource.APIModel{API: generic.API, Model: component.ControllerModel},
		resource.APIModel{API: toggleswitch.API, Model: component.LightModel},
		resource.APIModel{API: button.API, Model: component.SceneModel},
		resource.APIModel{API: discovery.API, Model: component.DiscoveryModel},
	)
}
