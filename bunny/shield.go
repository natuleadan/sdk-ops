package bunny

import (
	"context"
	"fmt"
)

// --- Base response wrappers ---

type ShieldResponse struct {
	Data  any    `json:"data,omitempty"`
	Error string `json:"error,omitempty"`
}

// --- Missing types ---

type GetShieldZonePullzoneMappingResponse struct {
	Data []map[string]any `json:"data"`
}

type GetCustomWafRulesResponse struct {
	Data  []CustomWAFRuleV2 `json:"data"`
	Page  int               `json:"page"`
	Error string            `json:"error,omitempty"`
}

type CustomAccessList struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Content     string `json:"content"`
}

type EventRow struct {
	LogID    string         `json:"logId"`
	Timestamp string        `json:"timestamp"`
	Log      string         `json:"log"`
	Fields   map[string]any `json:"fields"`
}

type EventLogsSearchResponse struct {
	Rows      []EventRow `json:"rows"`
	Groups    []any      `json:"groups"`
	Total     int        `json:"total"`
	TotalPages int       `json:"totalPages"`
	Page      int        `json:"page"`
}

// --- Shield Zone ---

type ShieldZoneV2 struct {
	ID                 int64  `json:"shieldZoneId"`
	PullZoneID         int64  `json:"pullZoneId"`
	WafEnabled         bool   `json:"wafEnabled"`
	LearningMode       bool   `json:"learningMode"`
	WafProfileID       int    `json:"wafProfileId"`
}

type CreateShieldZoneRequest struct {
	PullZoneID int64 `json:"pullZoneId"`
}

// --- Rate Limits v2 ---

type RateLimitV2 struct {
	ID                          int64  `json:"id"`
	ShieldZoneID                int64  `json:"shieldZoneId"`
	RuleName                    string `json:"ruleName"`
	BlockedRequests             int64  `json:"blockedRequests"`
	LoggedRequests              int64  `json:"loggedRequests"`
}

type CreateRateLimitRequest struct {
	ShieldZoneID    int64  `json:"shieldZoneId"`
	RuleName        string `json:"ruleName"`
	RuleDescription string `json:"ruleDescription"`
	RuleConfig      any    `json:"ruleConfiguration"`
}

// --- WAF ---

type WAFRuleV2 struct {
	ID              int64  `json:"id"`
	ShieldZoneID    int64  `json:"shieldZoneId"`
	RuleName        string `json:"ruleName"`
	ActionType      string `json:"actionType"`
	Enabled         bool   `json:"enabled"`
}

type CustomWAFRuleV2 struct {
	ID              int64  `json:"id"`
	RuleName        string `json:"ruleName"`
	RuleDescription string `json:"ruleDescription"`
	ShieldZoneID    int64  `json:"shieldZoneId"`
}

type CreateCustomWAFRuleRequest struct {
	ShieldZoneID    int64  `json:"shieldZoneId"`
	RuleName        string `json:"ruleName"`
	RuleDescription string `json:"ruleDescription"`
	RuleConfig      any    `json:"ruleConfiguration"`
}

type WAFProfile struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type WAFEnumMapping struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type WAFEngineConfig struct {
	Version string `json:"version"`
	Rules   []any  `json:"rules"`
}

// --- Access Lists ---

type AccessListDetails struct {
	ListID          int64  `json:"listId"`
	ConfigurationID int64  `json:"configurationId"`
	Name            string `json:"name"`
	IsEnabled       bool   `json:"isEnabled"`
}

type CreateCustomAccessListRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Content     string `json:"content"`
}

// --- API Guardian ---

type APIGuardianConfig struct {
	ShieldZoneID    int64  `json:"shieldZoneId"`
	IsEnabled       bool   `json:"isEnabled"`
	ExecutionMode   string `json:"executionMode"`
	BodyLimitAction string `json:"bodyLimitAction"`
}

type APIGuardianEndpoint struct {
	EndpointID  int64  `json:"apiGuardianEndpointId"`
	Method      string `json:"requestMethod"`
	Path        string `json:"requestPath"`
}

// --- Bot Categorization ---

type BotCategorizationGroup struct {
	Category  string  `json:"category"`
	GroupAction string `json:"categoryAction"`
	Bots      []BotCategorizationEntry `json:"bots"`
}

type BotCategorizationEntry struct {
	BotID        int64  `json:"botId"`
	UserAgent    string `json:"userAgentMatch"`
	Category     string `json:"category"`
	Action       string `json:"action"`
	IsVerifiable bool   `json:"isVerifiable"`
}

type BotCategoryAction struct {
	Category string `json:"category"`
	Action   string `json:"action"`
}

// --- Upload Scanning ---

type UploadScanningConfig struct {
	IsEnabled bool `json:"isEnabled"`
}

// --- Shield Zone methods ---

func (c *Client) ListShieldZoneConfigs(ctx context.Context) ([]ShieldZoneV2, error) {
	var resp struct {
		Data []ShieldZoneV2 `json:"data"`
		Page struct {
			TotalCount int `json:"totalCount"`
		} `json:"page"`
	}
	err := c.Get(ctx, APIShield, "/shield-zones", &resp)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *Client) GetShieldZoneConfig(ctx context.Context, id int64) (*ShieldZoneV2, error) {
	var resp struct {
		Data ShieldZoneV2 `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/shield-zone/%d", id), &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) GetShieldZoneByPullZone(ctx context.Context, pullZoneID int64) (*ShieldZoneV2, error) {
	var resp struct {
		Data ShieldZoneV2 `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/shield-zone/get-by-pullzone/%d", pullZoneID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) GetShieldZonesPullZoneMapping(ctx context.Context) ([]map[string]any, error) {
	var resp GetShieldZonePullzoneMappingResponse
	err := c.Get(ctx, APIShield, "/shield-zones/pullzone-mapping", &resp)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *Client) CreateShieldZoneV2(ctx context.Context, req CreateShieldZoneRequest) (*ShieldZoneV2, error) {
	var resp struct {
		Data ShieldZoneV2 `json:"data"`
	}
	err := c.Post(ctx, APIShield, "/shield-zone", req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) UpdateShieldZone(ctx context.Context, req ShieldZoneV2) (*ShieldZoneV2, error) {
	var resp struct {
		Data ShieldZoneV2 `json:"data"`
	}
	err := c.Patch(ctx, APIShield, "/shield-zone", req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) DeleteShieldZone(ctx context.Context, id int64) error {
	return c.Delete(ctx, APIShield, fmt.Sprintf("/shield-zone/%d", id), nil)
}

// --- WAF Rules ---

func (c *Client) GetWAFRules(ctx context.Context, zoneID int64) ([]WAFRuleV2, error) {
	var resp struct {
		Data []WAFRuleV2 `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/waf/rules/%d", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *Client) GetWAFRulesByPlan(ctx context.Context) ([]map[string]any, error) {
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	err := c.Get(ctx, APIShield, "/waf/rules/plan-segmentation", &resp)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *Client) GetTriggeredWAFRules(ctx context.Context, zoneID int64) ([]map[string]any, error) {
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/waf/rules/review-triggered/%d", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *Client) UpdateTriggeredWAFRule(ctx context.Context, zoneID int64, ruleID string, action string) error {
	return c.Post(ctx, APIShield, fmt.Sprintf("/waf/rules/review-triggered/%d", zoneID),
		map[string]string{"ruleId": ruleID, "action": action}, nil)
}

func (c *Client) GetTriggeredRuleAIRecommendation(ctx context.Context, zoneID int64, ruleID string) (*map[string]any, error) {
	var resp struct {
		Data map[string]any `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/waf/rules/review-triggered/ai-recommendation/%d/%s", zoneID, ruleID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) GetCustomWAFRules(ctx context.Context, zoneID int64) ([]CustomWAFRuleV2, error) {
	var resp GetCustomWafRulesResponse
	err := c.Get(ctx, APIShield, fmt.Sprintf("/waf/custom-rules/%d", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *Client) GetCustomWAFRule(ctx context.Context, ruleID int64) (*CustomWAFRuleV2, error) {
	var resp struct {
		Data CustomWAFRuleV2 `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/waf/custom-rule/%d", ruleID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) CreateCustomWAFRuleV2(ctx context.Context, req CreateCustomWAFRuleRequest) (*CustomWAFRuleV2, error) {
	var resp struct {
		Data CustomWAFRuleV2 `json:"data"`
	}
	err := c.Post(ctx, APIShield, "/waf/custom-rule", req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) UpdateCustomWAFRule(ctx context.Context, ruleID int64, req CreateCustomWAFRuleRequest) (*CustomWAFRuleV2, error) {
	var resp struct {
		Data CustomWAFRuleV2 `json:"data"`
	}
	err := c.Put(ctx, APIShield, fmt.Sprintf("/waf/custom-rule/%d", ruleID), req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) PatchCustomWAFRule(ctx context.Context, ruleID int64, req map[string]any) (*CustomWAFRuleV2, error) {
	var resp struct {
		Data CustomWAFRuleV2 `json:"data"`
	}
	err := c.Patch(ctx, APIShield, fmt.Sprintf("/waf/custom-rule/%d", ruleID), req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) DeleteCustomWAFRule(ctx context.Context, ruleID int64) error {
	return c.Delete(ctx, APIShield, fmt.Sprintf("/waf/custom-rule/%d", ruleID), nil)
}

func (c *Client) ListWAFProfiles(ctx context.Context) ([]WAFProfile, error) {
	var resp struct {
		Data [][]WAFProfile `json:"data"`
	}
	err := c.Get(ctx, APIShield, "/waf/profiles", &resp)
	if err != nil {
		return nil, err
	}
	var all []WAFProfile
	for _, group := range resp.Data {
		all = append(all, group...)
	}
	return all, nil
}

func (c *Client) GetWAFEnumMappings(ctx context.Context) ([]map[string]any, error) {
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	err := c.Get(ctx, APIShield, "/waf/enums", &resp)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *Client) GetWAFEngineConfig(ctx context.Context) ([]map[string]any, error) {
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	err := c.Get(ctx, APIShield, "/waf/engine-config", &resp)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// --- Rate Limits v2 ---

func (c *Client) GetRateLimitsForZone(ctx context.Context, zoneID int64) ([]RateLimitV2, error) {
	var resp struct {
		Data []RateLimitV2 `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/rate-limits/%d", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *Client) GetRateLimit(ctx context.Context, id int64) (*RateLimitV2, error) {
	var resp struct {
		Data RateLimitV2 `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/rate-limit/%d", id), &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) CreateRateLimitV2(ctx context.Context, req CreateRateLimitRequest) (*RateLimitV2, error) {
	var resp struct {
		Data RateLimitV2 `json:"data"`
	}
	err := c.Post(ctx, APIShield, "/rate-limit", req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) UpdateRateLimit(ctx context.Context, id int64, req map[string]any) (*RateLimitV2, error) {
	var resp struct {
		Data RateLimitV2 `json:"data"`
	}
	err := c.Patch(ctx, APIShield, fmt.Sprintf("/rate-limit/%d", id), req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) DeleteRateLimit(ctx context.Context, id int64) error {
	return c.Delete(ctx, APIShield, fmt.Sprintf("/rate-limit/%d", id), nil)
}

// --- Access Lists ---

func (c *Client) ListAccessLists(ctx context.Context, zoneID int64) (*AccessListDetails, error) {
	var resp struct {
		Data AccessListDetails `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/access-lists", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) CreateCustomAccessList(ctx context.Context, zoneID int64, req CreateCustomAccessListRequest) (*CustomAccessList, error) {
	var resp struct {
		Data CustomAccessList `json:"data"`
	}
	err := c.Post(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/access-lists", zoneID), req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) GetCustomAccessList(ctx context.Context, zoneID, listID int64) (*CustomAccessList, error) {
	var resp struct {
		Data CustomAccessList `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/access-lists/%d", zoneID, listID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) UpdateCustomAccessList(ctx context.Context, zoneID, listID int64, req map[string]any) (*CustomAccessList, error) {
	var resp struct {
		Data CustomAccessList `json:"data"`
	}
	err := c.Patch(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/access-lists/%d", zoneID, listID), req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) DeleteCustomAccessList(ctx context.Context, zoneID, listID int64) error {
	return c.Delete(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/access-lists/%d", zoneID, listID), nil)
}

func (c *Client) UpdateAccessListConfiguration(ctx context.Context, zoneID, cfgID int64, req map[string]any) error {
	return c.Patch(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/access-lists/configurations/%d", zoneID, cfgID), req, nil)
}

func (c *Client) GetAccessListEnums(ctx context.Context, zoneID int64) ([]map[string]any, error) {
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/access-lists/enums", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// --- Bot Detection (v2) ---

type BotDetectionConfigV2 struct {
	ShieldZoneID      int64  `json:"shieldZoneId"`
	ExecutionMode     string `json:"executionMode"`
	Sensitivity       string `json:"sensitivity"`
}

func (c *Client) GetBotDetectionConfig(ctx context.Context, zoneID int64) (*BotDetectionConfigV2, error) {
	var resp struct {
		Data BotDetectionConfigV2 `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/bot-detection", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) UpdateBotDetectionConfig(ctx context.Context, zoneID int64, cfg BotDetectionConfigV2) error {
	return c.Patch(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/bot-detection", zoneID), cfg, nil)
}

// --- Bot Categorization ---

func (c *Client) ListBotCategorizations(ctx context.Context, zoneID int64) ([]BotCategorizationGroup, error) {
	var resp struct {
		Data []BotCategorizationGroup `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/bot-categorization", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *Client) SetBotCategorizationAction(ctx context.Context, zoneID, botID int64, action string) error {
	return c.Put(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/bot-categorization/bots/%d", zoneID, botID),
		map[string]string{"action": action}, nil)
}

func (c *Client) SetBotCategoryAction(ctx context.Context, zoneID int64, category, action string) error {
	return c.Put(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/bot-categorization/categories/%s", zoneID, category),
		map[string]string{"action": action}, nil)
}

// --- API Guardian ---

func (c *Client) GetAPIGuardian(ctx context.Context, zoneID int64) (*APIGuardianConfig, error) {
	var resp struct {
		Data APIGuardianConfig `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/api-guardian", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) UpdateAPIGuardian(ctx context.Context, zoneID int64, cfg APIGuardianConfig) error {
	return c.Patch(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/api-guardian", zoneID), cfg, nil)
}

func (c *Client) UpdateAPIGuardianEndpoint(ctx context.Context, zoneID, endpointID int64, cfg map[string]any) error {
	return c.Patch(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/api-guardian/endpoint/%d", zoneID, endpointID), cfg, nil)
}

func (c *Client) UploadOpenAPISpec(ctx context.Context, zoneID int64, specBody string) error {
	return c.Post(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/api-guardian/spec", zoneID),
		map[string]string{"openApiSpec": specBody}, nil)
}

func (c *Client) UpdateOpenAPISpec(ctx context.Context, zoneID int64, specBody string) error {
	return c.Patch(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/api-guardian/spec", zoneID),
		map[string]string{"openApiSpec": specBody}, nil)
}

func (c *Client) GetAPIGuardianEnums(ctx context.Context, zoneID int64) ([]map[string]any, error) {
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/api-guardian/enums", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// --- Upload Scanning ---

func (c *Client) GetUploadScanning(ctx context.Context, zoneID int64) (*UploadScanningConfig, error) {
	var resp struct {
		Data UploadScanningConfig `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/upload-scanning", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) UpdateUploadScanning(ctx context.Context, zoneID int64, cfg UploadScanningConfig) error {
	return c.Patch(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/upload-scanning", zoneID), cfg, nil)
}

// --- Custom Response Pages ---

func (c *Client) UploadCustomResponsePage(ctx context.Context, zoneID int64, pageType, content string) error {
	return c.Put(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/custom-page/%s", zoneID, pageType),
		map[string]string{"content": content}, nil)
}

func (c *Client) GetCustomResponsePage(ctx context.Context, zoneID int64, pageType string) (string, error) {
	var resp struct {
		Data string `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/custom-page/%s", zoneID, pageType), &resp)
	if err != nil {
		return "", err
	}
	return resp.Data, nil
}

func (c *Client) DeleteCustomResponsePage(ctx context.Context, zoneID int64, pageType string) error {
	return c.Delete(ctx, APIShield, fmt.Sprintf("/shield-zone/%d/custom-page/%s", zoneID, pageType), nil)
}

// --- DDoS Enums ---

func (c *Client) GetDDoSEnums(ctx context.Context) ([]map[string]any, error) {
	var resp struct {
		Data []map[string]any `json:"data"`
	}
	err := c.Get(ctx, APIShield, "/ddos/enums", &resp)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// --- Promo ---

func (c *Client) GetPromotionState(ctx context.Context) (*map[string]any, error) {
	var resp struct {
		Data map[string]any `json:"data"`
	}
	err := c.Get(ctx, APIShield, "/promo/state", &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// --- Event Logs ---

func (c *Client) GetEventLogs(ctx context.Context, zoneID int64, date, continuationToken string) ([]EventRow, error) {
	path := fmt.Sprintf("/event-logs/%d/%s", zoneID, date)
	if continuationToken != "" {
		path += "/" + continuationToken
	}
	var resp struct {
		Data []EventRow `json:"data"`
	}
	err := c.Get(ctx, APIShield, path, &resp)
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *Client) SearchEventLogs(ctx context.Context, zoneID int64, req map[string]any) (*EventLogsSearchResponse, error) {
	var resp EventLogsSearchResponse
	err := c.Post(ctx, APIShield, fmt.Sprintf("/event-logs/%d/search", zoneID), req, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) ExportEventLogs(ctx context.Context, zoneID int64, req map[string]any) (string, error) {
	var resp struct {
		Data string `json:"data"`
	}
	err := c.Post(ctx, APIShield, fmt.Sprintf("/event-logs/%d/export", zoneID), req, &resp)
	if err != nil {
		return "", err
	}
	return resp.Data, nil
}

// --- Metrics (read-only) ---

func (c *Client) GetShieldZoneMonthlyOverages(ctx context.Context, zoneID int64) (*map[string]any, error) {
	var resp struct {
		Data map[string]any `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/metrics/overages/%d", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) GetShieldZoneMetricsOverview(ctx context.Context, zoneID int64) (*map[string]any, error) {
	var resp struct {
		Data map[string]any `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/metrics/overview/%d", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) GetShieldZoneDetailedMetrics(ctx context.Context, zoneID int64) (*map[string]any, error) {
	var resp struct {
		Data map[string]any `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/metrics/overview/%d/detailed", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) GetShieldZoneRateLimitMetrics(ctx context.Context, zoneID int64) (*map[string]any, error) {
	var resp struct {
		Data map[string]any `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/metrics/rate-limits/%d", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) GetRateLimitMetrics(ctx context.Context, id int64) (*map[string]any, error) {
	var resp struct {
		Data map[string]any `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/metrics/rate-limit/%d", id), &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) GetWAFRuleMetrics(ctx context.Context, zoneID, ruleID int64) (*map[string]any, error) {
	var resp struct {
		Data map[string]any `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/metrics/shield-zone/%d/waf-rule/%d", zoneID, ruleID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) GetShieldZoneBotDetectionMetrics(ctx context.Context, zoneID int64) (*map[string]any, error) {
	var resp struct {
		Data map[string]any `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/metrics/shield-zone/%d/bot-detection", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) GetShieldZoneUploadScanningMetrics(ctx context.Context, zoneID int64) (*map[string]any, error) {
	var resp struct {
		Data map[string]any `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/metrics/shield-zone/%d/upload-scanning", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) GetShieldZoneAPIGuardianMetrics(ctx context.Context, zoneID int64) (*map[string]any, error) {
	var resp struct {
		Data map[string]any `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/metrics/shield-zone/%d/api-guardian", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) GetAPIGuardianEndpointMetrics(ctx context.Context, zoneID, endpointID int64) (*map[string]any, error) {
	var resp struct {
		Data map[string]any `json:"data"`
	}
	err := c.Get(ctx, APIShield, fmt.Sprintf("/metrics/shield-zone/%d/api-guardian/endpoint/%d", zoneID, endpointID), &resp)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}
