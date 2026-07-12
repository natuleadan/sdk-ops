package bunny

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

type BillingDetails struct {
	Balance                       float64 `json:"Balance"`
	AvailableBalance              float64 `json:"AvailableBalance"`
	ThisMonthCharges              float64 `json:"ThisMonthCharges"`
	MonthlyBandwidthUsed          int64   `json:"MonthlyBandwidthUsed"`
	MonthlyChargesEUTraffic       float64 `json:"MonthlyChargesEUTraffic"`
	MonthlyChargesUSTraffic       float64 `json:"MonthlyChargesUSTraffic"`
	MonthlyChargesASIATraffic     float64 `json:"MonthlyChargesASIATraffic"`
	MonthlyChargesStorage         float64 `json:"MonthlyChargesStorage"`
	MonthlyChargesDNS             float64 `json:"MonthlyChargesDNS"`
	MonthlyChargesShield          float64 `json:"MonthlyChargesShield"`
	MonthlyChargesMagicContainers float64 `json:"MonthlyChargesMagicContainers"`
	MonthlyChargesScripting       float64 `json:"MonthlyChargesScripting"`
	MonthlyChargesOptimizer       float64 `json:"MonthlyChargesOptimizer"`
	MonthlyChargesTranscribe      float64 `json:"MonthlyChargesTranscribe"`
	MonthlyChargesPremiumEncoding float64 `json:"MonthlyChargesPremiumEncoding"`
	MonthlyChargesDrm             float64 `json:"MonthlyChargesDrm"`
	MonthlyChargesWebSockets      float64 `json:"MonthlyChargesWebSockets"`
	MonthlyChargesDB              float64 `json:"MonthlyChargesDB"`
	MonthlyChargesTaxes           float64 `json:"MonthlyChargesTaxes"`
	VATRate                       float64 `json:"VATRate"`
	BillingEnabled                bool    `json:"BillingEnabled"`
	AutomaticRechargeEnabled      bool    `json:"AutomaticRechargeEnabled"`
}

type BillingSummaryItem struct {
	PullZoneID             int64   `json:"PullZoneId"`
	MonthlyUsage           float64 `json:"MonthlyUsage"`
	MonthlyBandwidthUsed   int64   `json:"MonthlyBandwidthUsed"`
}

type PaymentRequest struct {
	ID          int64  `json:"Id"`
	Amount      float64 `json:"Amount"`
	Description string  `json:"Description"`
	Paid        bool    `json:"Paid"`
	Date        string  `json:"Date"`
}

func (c *Client) GetBillingDetails(ctx context.Context) (*BillingDetails, error) {
	var resp BillingDetails
	err := c.Get(ctx, APICore, "/billing", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetBillingSummary(ctx context.Context) ([]BillingSummaryItem, error) {
	var resp []BillingSummaryItem
	err := c.Get(ctx, APICore, "/billing/summary", &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetPaymentRequests(ctx context.Context) ([]PaymentRequest, error) {
	var resp []PaymentRequest
	err := c.Get(ctx, APICore, "/billing/payment-requests", &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetAffiliateDetails(ctx context.Context) (*map[string]any, error) {
	var resp map[string]any
	err := c.Get(ctx, APICore, "/billing/affiliate", &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetBillingInvoicePDF(ctx context.Context, recordID int64) ([]byte, error) {
	u := fmt.Sprintf("%s/billing/summary/%d/pdf", CoreAPIBase, recordID)
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil { return nil, err }
	req.Header.Set("AccessKey", c.apiKey)
	resp, err := c.httpClient.Do(req)
	if err != nil { return nil, err }
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("billing pdf: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
