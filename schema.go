package ojs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

// Schema represents a registered schema summary returned by list endpoints.
type Schema struct {
	URI       string    `json:"uri"`
	Type      string    `json:"type"`
	Version   string    `json:"version"`
	CreatedAt time.Time `json:"created_at"`
}

// SchemaDetail represents the full schema object returned by the get endpoint,
// including the JSON Schema definition itself.
type SchemaDetail struct {
	URI       string          `json:"uri"`
	Type      string          `json:"type"`
	Version   string          `json:"version"`
	Schema    json.RawMessage `json:"schema"`
	CreatedAt time.Time       `json:"created_at"`
}

// SchemaDefinition is the request body for registering a new schema.
type SchemaDefinition struct {
	URI     string          `json:"uri"`
	Type    string          `json:"type"`
	Version string          `json:"version"`
	Schema  json.RawMessage `json:"schema"`
}

// ListSchemas returns all registered schemas with pagination metadata.
func (c *Client) ListSchemas(ctx context.Context) ([]Schema, *Pagination, error) {
	var resp struct {
		Schemas    []Schema   `json:"schemas"`
		Pagination Pagination `json:"pagination"`
	}
	if err := c.transport.get(ctx, basePath+"/schemas", &resp); err != nil {
		return nil, nil, err
	}
	return resp.Schemas, &resp.Pagination, nil
}

// RegisterSchema registers a new schema definition with the server.
func (c *Client) RegisterSchema(ctx context.Context, schema SchemaDefinition) (*Schema, error) {
	var resp struct {
		Schema Schema `json:"schema"`
	}
	if err := c.transport.post(ctx, basePath+"/schemas", schema, &resp); err != nil {
		return nil, err
	}
	return &resp.Schema, nil
}

// GetSchema retrieves the full details of a schema by URI.
func (c *Client) GetSchema(ctx context.Context, uri string) (*SchemaDetail, error) {
	var resp struct {
		Schema SchemaDetail `json:"schema"`
	}
	path := fmt.Sprintf("%s/schemas/%s", basePath, url.PathEscape(uri))
	if err := c.transport.get(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp.Schema, nil
}

// DeleteSchema removes a registered schema by URI.
func (c *Client) DeleteSchema(ctx context.Context, uri string) error {
	path := fmt.Sprintf("%s/schemas/%s", basePath, url.PathEscape(uri))
	return c.transport.delete(ctx, path, nil)
}
