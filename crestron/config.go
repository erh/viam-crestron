package crestron

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// This file parses a Crestron Home processor's on-disk configuration, which can
// be pulled over SFTP from \user\Data\Configuration. It lets us enumerate every
// light load without the REST API's Web API token, which is useful for initial
// discovery and as a cross check against the live API.
//
// Two files are involved:
//   LightingSystem.cfg  the loads themselves (id, name, dimmable, room id)
//   SystemConfig.cfg    the room id -> room name mapping
//
// Both are Newtonsoft-serialized JSON with $id/$ref back references; we only
// read plain scalar fields, so the references can be ignored.

// ConfigLoad is one light load as described by LightingSystem.cfg.
type ConfigLoad struct {
	ID       int    `json:"ID"`
	Name     string `json:"name"`
	RoomID   int    `json:"assignedRoomID"`
	RoomName string `json:"-"`

	Dimmable bool `json:"lightIsDimmable"`
	// LightingLoadType: 0 dimmer, 1 switch, 2 fan.
	LightingLoadType int `json:"lightingLoadType"`
	MinDimLevel      int `json:"minDimLevel"`
	MaxDimLevel      int `json:"maxDimLevel"`
}

// Kind names the load type for display.
func (l ConfigLoad) Kind() string {
	switch l.LightingLoadType {
	case 0:
		return "dimmer"
	case 1:
		return "switch"
	case 2:
		return "fan"
	default:
		return fmt.Sprintf("type%d", l.LightingLoadType)
	}
}

type lightingSystem struct {
	Loads map[string]json.RawMessage `json:"loads"`
}

// ParseLightingConfig reads LightingSystem.cfg and SystemConfig.cfg (paths on
// the local disk, already fetched from the processor) and returns the loads
// with their room names filled in, sorted by room then name.
func ParseLightingConfig(lightingPath, systemConfigPath string) ([]ConfigLoad, error) {
	rooms, err := roomNames(systemConfigPath)
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(lightingPath)
	if err != nil {
		return nil, err
	}
	var ls lightingSystem
	if err := json.Unmarshal(data, &ls); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", lightingPath, err)
	}

	loads := make([]ConfigLoad, 0, len(ls.Loads))
	for _, raw := range ls.Loads {
		var l ConfigLoad
		if err := json.Unmarshal(raw, &l); err != nil || l.ID == 0 {
			continue // skip $type markers and malformed entries
		}
		l.RoomName = rooms[l.RoomID]
		loads = append(loads, l)
	}

	sort.Slice(loads, func(i, j int) bool {
		if loads[i].RoomName != loads[j].RoomName {
			return loads[i].RoomName < loads[j].RoomName
		}
		return loads[i].Name < loads[j].Name
	})
	return loads, nil
}

// roomNames walks SystemConfig.cfg for objects that carry an integer ID and a
// name but are not light loads, which is how rooms are represented there.
func roomNames(systemConfigPath string) (map[int]string, error) {
	data, err := os.ReadFile(systemConfigPath)
	if err != nil {
		return nil, err
	}
	var root any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", systemConfigPath, err)
	}
	rooms := map[int]string{}
	var walk func(any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			id, hasID := t["ID"].(float64)
			name, hasName := t["name"].(string)
			_, isLoad := t["loadIndex"]
			if hasID && hasName && !isLoad && name != "" {
				if iid := int(id); iid > 52000 && iid < 53100 {
					if _, seen := rooms[iid]; !seen {
						rooms[iid] = name
					}
				}
			}
			for _, c := range t {
				walk(c)
			}
		case []any:
			for _, c := range t {
				walk(c)
			}
		}
	}
	walk(root)
	return rooms, nil
}
