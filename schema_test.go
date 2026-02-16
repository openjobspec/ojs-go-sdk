package ojs

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListSchemas(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/ojs/v1/schemas" {
			t.Errorf("expected /ojs/v1/schemas, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", ojsContentType)
		json.NewEncoder(w).Encode(map[string]any{
			"schemas": []map[string]any{
				{"uri": "urn:ojs:schema:email.send:v1", "type": "email.send", "version": "1.0.0", "created_at": "2026-01-01T00:00:00Z"},
				{"uri": "urn:ojs:schema:report.generate:v1", "type": "report.generate", "version": "1.0.0", "created_at": "2026-01-02T00:00:00Z"},
			},
			"pagination": map[string]any{
				"total": 2, "limit": 50, "offset": 0, "has_more": false,
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	schemas, pagination, err := client.ListSchemas(context.Background())
	if err != nil {
		t.Fatalf("ListSchemas() error = %v", err)
	}
	if len(schemas) != 2 {
		t.Fatalf("expected 2 schemas, got %d", len(schemas))
	}
	if schemas[0].URI != "urn:ojs:schema:email.send:v1" {
		t.Errorf("expected first schema URI=urn:ojs:schema:email.send:v1, got %s", schemas[0].URI)
	}
	if schemas[0].Type != "email.send" {
		t.Errorf("expected first schema type=email.send, got %s", schemas[0].Type)
	}
	if schemas[1].Version != "1.0.0" {
		t.Errorf("expected second schema version=1.0.0, got %s", schemas[1].Version)
	}
	if pagination.Total != 2 {
		t.Errorf("expected pagination total=2, got %d", pagination.Total)
	}
	if pagination.Limit != 50 {
		t.Errorf("expected pagination limit=50, got %d", pagination.Limit)
	}
}

func TestRegisterSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/ojs/v1/schemas" {
			t.Errorf("expected /ojs/v1/schemas, got %s", r.URL.Path)
		}

		var req SchemaDefinition
		json.NewDecoder(r.Body).Decode(&req)
		if req.URI != "urn:ojs:schema:email.send:v1" {
			t.Errorf("expected uri=urn:ojs:schema:email.send:v1, got %s", req.URI)
		}
		if req.Type != "email.send" {
			t.Errorf("expected type=email.send, got %s", req.Type)
		}
		if req.Version != "1.0.0" {
			t.Errorf("expected version=1.0.0, got %s", req.Version)
		}

		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]any{
			"schema": map[string]any{
				"uri": "urn:ojs:schema:email.send:v1", "type": "email.send",
				"version": "1.0.0", "created_at": "2026-01-01T00:00:00Z",
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	schema, err := client.RegisterSchema(context.Background(), SchemaDefinition{
		URI:     "urn:ojs:schema:email.send:v1",
		Type:    "email.send",
		Version: "1.0.0",
		Schema:  json.RawMessage(`{"type":"object","properties":{"to":{"type":"string"}},"required":["to"]}`),
	})
	if err != nil {
		t.Fatalf("RegisterSchema() error = %v", err)
	}
	if schema.URI != "urn:ojs:schema:email.send:v1" {
		t.Errorf("expected uri=urn:ojs:schema:email.send:v1, got %s", schema.URI)
	}
	if schema.Type != "email.send" {
		t.Errorf("expected type=email.send, got %s", schema.Type)
	}
}

func TestGetSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/ojs/v1/schemas/urn:ojs:schema:email.send:v1" {
			t.Errorf("expected /ojs/v1/schemas/urn:ojs:schema:email.send:v1, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", ojsContentType)
		json.NewEncoder(w).Encode(map[string]any{
			"schema": map[string]any{
				"uri": "urn:ojs:schema:email.send:v1", "type": "email.send",
				"version": "1.0.0", "created_at": "2026-01-01T00:00:00Z",
				"schema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"to": map[string]any{"type": "string"},
					},
					"required": []any{"to"},
				},
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	detail, err := client.GetSchema(context.Background(), "urn:ojs:schema:email.send:v1")
	if err != nil {
		t.Fatalf("GetSchema() error = %v", err)
	}
	if detail.URI != "urn:ojs:schema:email.send:v1" {
		t.Errorf("expected uri=urn:ojs:schema:email.send:v1, got %s", detail.URI)
	}
	if detail.Type != "email.send" {
		t.Errorf("expected type=email.send, got %s", detail.Type)
	}
	if detail.Version != "1.0.0" {
		t.Errorf("expected version=1.0.0, got %s", detail.Version)
	}
	if len(detail.Schema) == 0 {
		t.Error("expected non-empty schema definition")
	}
}

func TestDeleteSchema(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/ojs/v1/schemas/urn:ojs:schema:email.send:v1" {
			t.Errorf("expected /ojs/v1/schemas/urn:ojs:schema:email.send:v1, got %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	err := client.DeleteSchema(context.Background(), "urn:ojs:schema:email.send:v1")
	if err != nil {
		t.Fatalf("DeleteSchema() error = %v", err)
	}
}

func TestGetSchemaNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", ojsContentType)
		w.WriteHeader(http.StatusNotFound)
		json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"code":    "not_found",
				"message": "Schema not found.",
			},
		})
	}))
	defer server.Close()

	client, _ := NewClient(server.URL)
	_, err := client.GetSchema(context.Background(), "urn:ojs:schema:nonexistent:v1")
	if err == nil {
		t.Fatal("expected error for nonexistent schema")
	}
}
