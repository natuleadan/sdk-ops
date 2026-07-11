package bunny

import (
	"context"
	"fmt"
)

// EdgeScript model

type EdgeScript struct {
	ID        int64  `json:"Id"`
	Name      string `json:"Name"`
	Type      int32  `json:"Type"`
	DateCreated string `json:"DateCreated,omitempty"`
	DateModified string `json:"DateModified,omitempty"`
}

type AddEdgeScriptModel struct {
	Name string `json:"Name"`
	Type int32  `json:"Type"`
}

type UpdateEdgeScriptModel struct {
	Name       string  `json:"Name,omitempty"`
	Content    *string `json:"Content,omitempty"`
}

type EdgeScriptCodeModel struct {
	Content string `json:"Content,omitempty"`
}

type UpdateEdgeScriptCodeModel struct {
	Content string `json:"Content"`
}

type EdgeScriptVariable struct {
	ID    int64  `json:"Id,omitempty"`
	Name  string `json:"Name"`
	Value string `json:"Value,omitempty"`
}

type EdgeScriptSecret struct {
	ID    int64  `json:"Id,omitempty"`
	Name  string `json:"Name"`
	Value string `json:"Value,omitempty"`
}

type EdgeScriptRelease struct {
	ID        int64  `json:"Id"`
	Version   int32  `json:"Version"`
	Status    int32  `json:"Status"` // 0=Archived, 1=Live
	Date      string `json:"Date,omitempty"`
}

type EdgeScriptStatistics struct {
	Requests     int64   `json:"requests,omitempty"`
	Bandwidth    int64   `json:"bandwidth,omitempty"`
	AvgCPU       float64 `json:"avgCpu,omitempty"`
	AvgMemory    float64 `json:"avgMemory,omitempty"`
}

// ListEdgeScriptsResponse
type ListEdgeScriptsResponse struct {
	Items []EdgeScript `json:"Items,omitempty"`
	Total int64        `json:"TotalItems,omitempty"`
}

// --- CRUD Scripts ---

func (c *Client) CreateEdgeScript(ctx context.Context, req AddEdgeScriptModel) (*EdgeScript, error) {
	var resp struct {
		ID int64 `json:"Id"`
	}
	err := c.Post(ctx, APICore, "/compute/script", req, &resp)
	if err != nil {
		return nil, err
	}
	return &EdgeScript{ID: resp.ID, Name: req.Name, Type: req.Type}, nil
}

func (c *Client) ListEdgeScripts(ctx context.Context) ([]EdgeScript, error) {
	var resp ListEdgeScriptsResponse
	err := c.Get(ctx, APICore, "/compute/script", &resp)
	if err != nil {
		return nil, err
	}
	if resp.Items == nil {
		return []EdgeScript{}, nil
	}
	return resp.Items, nil
}

func (c *Client) GetEdgeScript(ctx context.Context, id int64) (*EdgeScript, error) {
	var resp EdgeScript
	err := c.Get(ctx, APICore, fmt.Sprintf("/compute/script/%d", id), &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) UpdateEdgeScript(ctx context.Context, id int64, req UpdateEdgeScriptModel) (*EdgeScript, error) {
	var resp EdgeScript
	err := c.Post(ctx, APICore, fmt.Sprintf("/compute/script/%d", id), req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DeleteEdgeScript(ctx context.Context, id int64) error {
	return c.Delete(ctx, APICore, fmt.Sprintf("/compute/script/%d", id), nil)
}

func (c *Client) RotateDeploymentKey(ctx context.Context, id int64) error {
	return c.Post(ctx, APICore, fmt.Sprintf("/compute/script/%d/deploymentKey/rotate", id), nil, nil)
}

// --- Code ---

func (c *Client) GetEdgeScriptCode(ctx context.Context, id int64) (*EdgeScriptCodeModel, error) {
	var resp EdgeScriptCodeModel
	err := c.Get(ctx, APICore, fmt.Sprintf("/compute/script/%d/code", id), &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) SetEdgeScriptCode(ctx context.Context, id int64, content string) error {
	return c.Post(ctx, APICore, fmt.Sprintf("/compute/script/%d/code", id),
		UpdateEdgeScriptCodeModel{Content: content}, nil)
}

// --- Variables ---

func (c *Client) AddEdgeScriptVariable(ctx context.Context, id int64, name, value string) (*EdgeScriptVariable, error) {
	var resp EdgeScriptVariable
	err := c.Post(ctx, APICore, fmt.Sprintf("/compute/script/%d/variables/add", id),
		EdgeScriptVariable{Name: name, Value: value}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) UpsertEdgeScriptVariable(ctx context.Context, id int64, name, value string) (*EdgeScriptVariable, error) {
	var resp EdgeScriptVariable
	err := c.Put(ctx, APICore, fmt.Sprintf("/compute/script/%d/variables", id),
		EdgeScriptVariable{Name: name, Value: value}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DeleteEdgeScriptVariable(ctx context.Context, scriptID, varID int64) error {
	return c.Delete(ctx, APICore, fmt.Sprintf("/compute/script/%d/variables/%d", scriptID, varID), nil)
}

// --- Secrets ---

func (c *Client) AddEdgeScriptSecret(ctx context.Context, id int64, name, value string) (*EdgeScriptSecret, error) {
	var resp EdgeScriptSecret
	err := c.Post(ctx, APICore, fmt.Sprintf("/compute/script/%d/secrets", id),
		EdgeScriptSecret{Name: name, Value: value}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) UpsertEdgeScriptSecret(ctx context.Context, id int64, name, value string) (*EdgeScriptSecret, error) {
	var resp EdgeScriptSecret
	err := c.Put(ctx, APICore, fmt.Sprintf("/compute/script/%d/secrets", id),
		EdgeScriptSecret{Name: name, Value: value}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListEdgeScriptSecrets(ctx context.Context, id int64) ([]EdgeScriptSecret, error) {
	var resp struct {
		Secrets []EdgeScriptSecret `json:"Secrets,omitempty"`
	}
	err := c.Get(ctx, APICore, fmt.Sprintf("/compute/script/%d/secrets", id), &resp)
	if err != nil {
		return nil, err
	}
	return resp.Secrets, nil
}

func (c *Client) DeleteEdgeScriptSecret(ctx context.Context, scriptID, secretID int64) error {
	return c.Delete(ctx, APICore, fmt.Sprintf("/compute/script/%d/secrets/%d", scriptID, secretID), nil)
}

// --- Releases ---

func (c *Client) GetActiveRelease(ctx context.Context, id int64) (*EdgeScriptRelease, error) {
	var resp EdgeScriptRelease
	err := c.Get(ctx, APICore, fmt.Sprintf("/compute/script/%d/releases/active", id), &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) PublishRelease(ctx context.Context, id int64) error {
	return c.Post(ctx, APICore, fmt.Sprintf("/compute/script/%d/publish", id), nil, nil)
}

// --- Statistics ---

func (c *Client) GetEdgeScriptStatistics(ctx context.Context, id int64) (*EdgeScriptStatistics, error) {
	var resp EdgeScriptStatistics
	err := c.Get(ctx, APICore, fmt.Sprintf("/compute/script/%d/statistics", id), &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}
