package bunny

import (
	"context"
	"fmt"
)

type ExternalDnsCertRequest struct {
	Hostname string `json:"hostname"`
}

type ExternalDnsCertResponse struct {
	Hostname             string `json:"hostname"`
	Token                string `json:"token"`
	VerificationHostname string `json:"verificationHostname"`
}

type PrivateKeyUpdateModel struct {
	Hostname     string `json:"hostname"`
	KeyType      int    `json:"keyType"`
}

type ForceSSLModel struct {
	Hostname  string `json:"hostname"`
	ForceSSL  bool   `json:"forceSSL"`
}

type ReferrerModel struct {
	Hostname string `json:"hostname"`
	Referrer string `json:"referrer"`
}

type BlockedIPModel struct {
	BlockedIP string `json:"blockedIp"`
}

type ResetSecurityKeyModel struct {
	SecurityKey string `json:"securityKey,omitempty"`
}

type ZoneAvailabilityResponse struct {
	Available bool `json:"Available"`
}

func (c *Client) ListPullZones(ctx context.Context) ([]PullZone, error) {
	var zones []PullZone
	err := c.Get(ctx, APICore, "/pullzone", &zones)
	if err != nil {
		return nil, err
	}
	return zones, nil
}

func (c *Client) GetPullZone(ctx context.Context, zoneID int64) (*PullZone, error) {
	var zone PullZone
	err := c.Get(ctx, APICore, fmt.Sprintf("/pullzone/%d", zoneID), &zone)
	if err != nil {
		return nil, err
	}
	return &zone, nil
}

func (c *Client) CreatePullZone(ctx context.Context, model PullZoneAddModel) error {
	return c.Post(ctx, APICore, "/pullzone", model, nil)
}

func (c *Client) UpdatePullZone(ctx context.Context, zoneID int64, model PullZoneSettingsModel) error {
	return c.Post(ctx, APICore, fmt.Sprintf("/pullzone/%d", zoneID), model, nil)
}

func (c *Client) DeletePullZone(ctx context.Context, zoneID int64) error {
	return c.Delete(ctx, APICore, fmt.Sprintf("/pullzone/%d", zoneID), nil)
}

func (c *Client) PurgePullZoneCache(ctx context.Context, zoneID int64, url string) error {
	var model *PullZonePurgeModel
	if url != "" {
		model = &PullZonePurgeModel{URL: url}
	}
	return c.Post(ctx, APICore, fmt.Sprintf("/pullzone/%d/purgeCache", zoneID), model, nil)
}

func (c *Client) CountPullZones(ctx context.Context) (int64, error) {
	var count PullZoneCount
	err := c.Get(ctx, APICore, "/pullzone/count", &count)
	if err != nil {
		return 0, err
	}
	return count.Count, nil
}

func (c *Client) LoadFreeCertificate(ctx context.Context, hostname string, useHTTP01 bool) error {
	q := fmt.Sprintf("?hostname=%s&useOnlyHttp01=%t", hostname, useHTTP01)
	return c.Get(ctx, APICore, "/pullzone/loadFreeCertificate"+q, nil)
}

func (c *Client) AddPullZoneHostname(ctx context.Context, zoneID int64, hostname string) error {
	return c.Post(ctx, APICore, fmt.Sprintf("/pullzone/%d/addHostname", zoneID),
		AddHostnameRequestModel{Hostname: hostname}, nil)
}

func (c *Client) RemovePullZoneHostname(ctx context.Context, zoneID int64, hostname string) error {
	return c.Delete(ctx, APICore, fmt.Sprintf("/pullzone/%d/removeHostname", zoneID),
		RemoveHostnameRequestModel{Hostname: hostname})
}

func (c *Client) AddPullZoneCertificate(ctx context.Context, zoneID int64, cert, key string) error {
	return c.Post(ctx, APICore, fmt.Sprintf("/pullzone/%d/addCertificate", zoneID),
		map[string]string{"certificate": cert, "certificateKey": key}, nil)
}

func (c *Client) RemovePullZoneCertificate(ctx context.Context, zoneID int64, hostname string) error {
	return c.Delete(ctx, APICore, fmt.Sprintf("/pullzone/%d/removeCertificate", zoneID),
		RemoveHostnameRequestModel{Hostname: hostname})
}

func (c *Client) AddEdgeRule(ctx context.Context, zoneID int64, rule EdgeRule) error {
	return c.Post(ctx, APICore, fmt.Sprintf("/pullzone/%d/edgerules/addOrUpdate", zoneID), rule, nil)
}

func (c *Client) DeleteEdgeRule(ctx context.Context, zoneID int64, ruleID string) error {
	return c.Delete(ctx, APICore, fmt.Sprintf("/pullzone/%d/edgerules/%s", zoneID, ruleID), nil)
}

func (c *Client) SetEdgeRuleEnabled(ctx context.Context, zoneID int64, ruleID string, enabled bool) error {
	return c.Post(ctx, APICore, fmt.Sprintf("/pullzone/%d/edgerules/%s/setEdgeRuleEnabled", zoneID, ruleID),
		map[string]bool{"enabled": enabled}, nil)
}

func (c *Client) UpdatePrivateKeyType(ctx context.Context, zoneID int64, hostname string, keyType int) error {
	return c.Post(ctx, APICore, fmt.Sprintf("/pullzone/%d/updatePrivateKeyType", zoneID),
		PrivateKeyUpdateModel{Hostname: hostname, KeyType: keyType}, nil)
}

func (c *Client) RequestExternalDnsCertificate(ctx context.Context, hostname string) (*ExternalDnsCertResponse, error) {
	var resp ExternalDnsCertResponse
	err := c.Post(ctx, APICore, "/pullzone/requestExternalDnsCertificate",
		ExternalDnsCertRequest{Hostname: hostname}, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) CompleteExternalDnsCertificate(ctx context.Context, hostname string) error {
	return c.Post(ctx, APICore, "/pullzone/completeExternalDnsCertificate",
		ExternalDnsCertRequest{Hostname: hostname}, nil)
}

func (c *Client) CheckPullZoneAvailability(ctx context.Context, name string) (bool, error) {
	var resp ZoneAvailabilityResponse
	err := c.Post(ctx, APICore, "/pullzone/checkavailability",
		map[string]string{"name": name}, &resp)
	if err != nil {
		return false, err
	}
	return resp.Available, nil
}

func (c *Client) SetForceSSL(ctx context.Context, zoneID int64, hostname string, forceSSL bool) error {
	return c.Post(ctx, APICore, fmt.Sprintf("/pullzone/%d/setForceSSL", zoneID),
		ForceSSLModel{Hostname: hostname, ForceSSL: forceSSL}, nil)
}

func (c *Client) ResetSecurityKey(ctx context.Context, zoneID int64, securityKey string) error {
	model := ResetSecurityKeyModel{SecurityKey: securityKey}
	if securityKey == "" {
		return c.Post(ctx, APICore, fmt.Sprintf("/pullzone/%d/resetSecurityKey", zoneID), nil, nil)
	}
	return c.Post(ctx, APICore, fmt.Sprintf("/pullzone/%d/resetSecurityKey", zoneID), model, nil)
}

func (c *Client) AddAllowedReferrer(ctx context.Context, zoneID int64, hostname, referrer string) error {
	return c.Post(ctx, APICore, fmt.Sprintf("/pullzone/%d/addAllowedReferrer", zoneID),
		ReferrerModel{Hostname: hostname, Referrer: referrer}, nil)
}

func (c *Client) RemoveAllowedReferrer(ctx context.Context, zoneID int64, hostname, referrer string) error {
	return c.Post(ctx, APICore, fmt.Sprintf("/pullzone/%d/removeAllowedReferrer", zoneID),
		ReferrerModel{Hostname: hostname, Referrer: referrer}, nil)
}

func (c *Client) AddBlockedReferrer(ctx context.Context, zoneID int64, hostname, referrer string) error {
	return c.Post(ctx, APICore, fmt.Sprintf("/pullzone/%d/addBlockedReferrer", zoneID),
		ReferrerModel{Hostname: hostname, Referrer: referrer}, nil)
}

func (c *Client) RemoveBlockedReferrer(ctx context.Context, zoneID int64, hostname, referrer string) error {
	return c.Post(ctx, APICore, fmt.Sprintf("/pullzone/%d/removeBlockedReferrer", zoneID),
		ReferrerModel{Hostname: hostname, Referrer: referrer}, nil)
}

func (c *Client) AddBlockedIP(ctx context.Context, zoneID int64, ip string) error {
	return c.Post(ctx, APICore, fmt.Sprintf("/pullzone/%d/addBlockedIp", zoneID),
		BlockedIPModel{BlockedIP: ip}, nil)
}

func (c *Client) RemoveBlockedIP(ctx context.Context, zoneID int64, ip string) error {
	return c.Post(ctx, APICore, fmt.Sprintf("/pullzone/%d/removeBlockedIp", zoneID),
		BlockedIPModel{BlockedIP: ip}, nil)
}
