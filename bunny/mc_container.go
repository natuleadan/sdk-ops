package bunny

import (
	"context"
	"fmt"
)

func (c *Client) AddContainerTemplate(ctx context.Context, appID string, req ContainerRequest) (*AddApplicationResponse, error) {
	var resp AddApplicationResponse
	err := c.Post(ctx, APIMC, fmt.Sprintf("/apps/%s/containers", appID), req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetContainerTemplate(ctx context.Context, appID, containerID string) (*ContainerTemplate, error) {
	var resp ContainerTemplate
	err := c.Get(ctx, APIMC, fmt.Sprintf("/apps/%s/containers/%s", appID, containerID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) PatchContainerTemplate(ctx context.Context, appID, containerID string, req ContainerRequest) error {
	return c.Patch(ctx, APIMC, fmt.Sprintf("/apps/%s/containers/%s", appID, containerID), req, nil)
}

func (c *Client) DeleteContainerTemplate(ctx context.Context, appID, containerID string) error {
	return c.Delete(ctx, APIMC, fmt.Sprintf("/apps/%s/containers/%s", appID, containerID), nil)
}

func (c *Client) SetContainerEnvVars(ctx context.Context, appID, containerID string, envs []EnvironmentVariable) error {
	return c.Put(ctx, APIMC, fmt.Sprintf("/apps/%s/containers/%s/env", appID, containerID),
		map[string]any{"environmentVariables": envs}, nil)
}

func (c *Client) GetImageConfig(ctx context.Context, req ImageConfigRequest) (*ImageConfig, error) {
	var resp ImageConfig
	err := c.Post(ctx, APIMC, "/registries/image-config", req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetContainerConfigSuggestions(ctx context.Context, req ContainerConfigRequest) (*ContainerConfigSuggestions, error) {
	var resp ContainerConfigSuggestions
	err := c.Post(ctx, APIMC, "/registries/config-suggestions", req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

type ImageConfigRequest struct {
	RegistryID     string `json:"registryId"`
	ImageName      string `json:"imageName"`
	ImageNamespace string `json:"imageNamespace"`
	Tag            string `json:"tag,omitempty"`
}

type ContainerConfigRequest struct {
	RegistryID     string `json:"registryId"`
	ImageName      string `json:"imageName"`
	ImageNamespace string `json:"imageNamespace"`
	Tag            string `json:"tag,omitempty"`
}

type ContainerConfigSuggestions struct {
	EndpointSuggestions      []EndpointRequest             `json:"endpointSuggestions,omitempty"`
	EnvironmentVariablesSuggestions []EnvVarSuggestion      `json:"environmentVariablesSuggestions,omitempty"`
	VolumeSuggestions        []VolumeSuggestion             `json:"volumeSuggestions,omitempty"`
}

type EnvVarSuggestion struct {
	Name         string  `json:"name,omitempty"`
	DefaultValue *string `json:"defaultValue,omitempty"`
	Description  *string `json:"description,omitempty"`
	Required     bool    `json:"required,omitempty"`
}
