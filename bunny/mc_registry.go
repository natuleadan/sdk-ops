package bunny

import (
	"context"
	"fmt"
)

func (c *Client) ListContainerRegistries(ctx context.Context) (*ListContainerRegistriesResponse, error) {
	var resp ListContainerRegistriesResponse
	err := c.Get(ctx, APIMC, "/registries", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) AddContainerRegistry(ctx context.Context, req ContainerRegistryRequest) (*SaveContainerRegistryResult, error) {
	var resp SaveContainerRegistryResult
	err := c.Post(ctx, APIMC, "/registries", req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetContainerRegistry(ctx context.Context, registryID int64) (*ContainerRegistry, error) {
	var resp ContainerRegistry
	err := c.Get(ctx, APIMC, fmt.Sprintf("/registries/%d", registryID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) UpdateContainerRegistry(ctx context.Context, registryID int64, req ContainerRegistryRequest) (*SaveContainerRegistryResult, error) {
	var resp SaveContainerRegistryResult
	err := c.Put(ctx, APIMC, fmt.Sprintf("/registries/%d", registryID), req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) DeleteContainerRegistry(ctx context.Context, registryID int64) (*RemoveContainerRegistryResult, error) {
	var resp RemoveContainerRegistryResult
	err := c.Delete(ctx, APIMC, fmt.Sprintf("/registries/%d", registryID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ListContainerImages(ctx context.Context, registryID string) ([]ContainerImage, error) {
	var resp []ContainerImage
	err := c.Post(ctx, APIMC, "/registries/images", map[string]string{"registryId": registryID}, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) ListContainerImageTags(ctx context.Context, registryID, imageName, namespace string) ([]ContainerImageTag, error) {
	var resp []ContainerImageTag
	err := c.Post(ctx, APIMC, "/registries/tags", map[string]string{
		"registryId":    registryID,
		"imageName":     imageName,
		"imageNamespace": namespace,
	}, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) SearchPublicImages(ctx context.Context, req SearchPublicContainerImagesRequest) ([]ContainerImage, error) {
	var resp []ContainerImage
	err := c.Post(ctx, APIMC, "/registries/public-images/search", req, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetImageDigest(ctx context.Context, registryID, imageName, namespace, tag string) (*ImageTagInfo, error) {
	var resp ImageTagInfo
	err := c.Post(ctx, APIMC, "/registries/digest", map[string]string{
		"registryId":    registryID,
		"imageName":     imageName,
		"imageNamespace": namespace,
		"tag":           tag,
	}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}
