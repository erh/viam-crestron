package crestron

import (
	"context"
	"fmt"
	"net/http"
)

// Room is a Crestron Home room. Lights and other devices belong to one.
type Room struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Light is one controllable load: a dimmer, a switch, or a keypad load.
type Light struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	RoomID int    `json:"roomId"`

	// SubType distinguishes "Dimmer" from "Switch"; switches only accept 0
	// and MaxLevel.
	SubType string `json:"subType"`

	// Level is 0..MaxLevel.
	Level int `json:"level"`

	ConnectionStatus string `json:"connectionStatus"`
}

// On reports whether the load is drawing at all.
func (l Light) On() bool { return l.Level > 0 }

// Dimmable reports whether the load accepts intermediate levels.
func (l Light) Dimmable() bool { return l.SubType != "Switch" }

// Percent is the level as 0..100.
func (l Light) Percent() int { return int(float64(l.Level)/MaxLevel*100 + 0.5) }

// Rooms lists the rooms defined on the processor.
func (c *Client) Rooms(ctx context.Context) ([]Room, error) {
	var out struct {
		Rooms []Room `json:"rooms"`
	}
	if err := c.Do(ctx, http.MethodGet, "/rooms", nil, &out); err != nil {
		return nil, err
	}
	return out.Rooms, nil
}

// Lights lists every light load on the processor.
func (c *Client) Lights(ctx context.Context) ([]Light, error) {
	var out struct {
		Lights []Light `json:"lights"`
	}
	if err := c.Do(ctx, http.MethodGet, "/lights", nil, &out); err != nil {
		return nil, err
	}
	return out.Lights, nil
}

// Light fetches a single load by id.
func (c *Client) Light(ctx context.Context, id int) (Light, error) {
	var out struct {
		Lights []Light `json:"lights"`
	}
	if err := c.Do(ctx, http.MethodGet, fmt.Sprintf("/lights/%d", id), nil, &out); err != nil {
		return Light{}, err
	}
	if len(out.Lights) == 0 {
		return Light{}, fmt.Errorf("no light with id %d", id)
	}
	return out.Lights[0], nil
}

// levelRequest is one entry in a /lights/setstate call.
type levelRequest struct {
	ID    int `json:"id"`
	Level int `json:"level"`
	// Time is the fade duration in tenths of a second.
	Time int `json:"time"`
}

// SetLevel drives a load to level (0..MaxLevel), fading over fadeTenths tenths
// of a second.
func (c *Client) SetLevel(ctx context.Context, id, level, fadeTenths int) error {
	if level < 0 {
		level = 0
	}
	if level > MaxLevel {
		level = MaxLevel
	}
	body := struct {
		Lights []levelRequest `json:"lights"`
	}{[]levelRequest{{ID: id, Level: level, Time: fadeTenths}}}
	return c.Do(ctx, http.MethodPost, "/lights/setstate", body, nil)
}

// SetPercent drives a load to a 0..100 percentage.
func (c *Client) SetPercent(ctx context.Context, id, pct, fadeTenths int) error {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	return c.SetLevel(ctx, id, pct*MaxLevel/100, fadeTenths)
}

// TurnOn drives a load fully on.
func (c *Client) TurnOn(ctx context.Context, id int) error {
	return c.SetLevel(ctx, id, MaxLevel, 0)
}

// TurnOff drives a load off.
func (c *Client) TurnOff(ctx context.Context, id int) error {
	return c.SetLevel(ctx, id, 0, 0)
}
