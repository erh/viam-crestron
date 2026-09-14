package crestron

import (
	"context"
	"fmt"
	"net/http"
)

// Scene is a Crestron Home scene (a lighting/room preset that can be recalled).
type Scene struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	RoomID int    `json:"roomId"`
	// Status reports whether the scene is currently active.
	Status bool   `json:"status"`
	Type   string `json:"type"`
}

// Scenes lists every scene on the processor.
func (c *Client) Scenes(ctx context.Context) ([]Scene, error) {
	var out struct {
		Scenes []Scene `json:"scenes"`
	}
	if err := c.Do(ctx, http.MethodGet, "/scenes", nil, &out); err != nil {
		return nil, err
	}
	return out.Scenes, nil
}

// RecallScene activates the scene with the given id.
func (c *Client) RecallScene(ctx context.Context, id int) error {
	return c.Do(ctx, http.MethodPost, fmt.Sprintf("/scenes/recall/%d", id), nil, nil)
}
