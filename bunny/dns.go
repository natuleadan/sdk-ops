package bunny

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
)

func (c *Client) ListDNSZones(ctx context.Context) ([]DNSZone, error) {
	var resp struct {
		Items []DNSZone `json:"Items"`
	}
	err := c.Get(ctx, APICore, "/dnszone", &resp)
	if err != nil {
		return nil, err
	}
	if resp.Items == nil {
		return []DNSZone{}, nil
	}
	return resp.Items, nil
}

func (c *Client) GetDNSZone(ctx context.Context, zoneID int64) (*DNSZone, error) {
	var zone DNSZone
	err := c.Get(ctx, APICore, fmt.Sprintf("/dnszone/%d", zoneID), &zone)
	if err != nil {
		return nil, err
	}
	return &zone, nil
}

func (c *Client) AddDNSZone(ctx context.Context, domain string) error {
	return c.Post(ctx, APICore, "/dnszone", DNSZoneAddModel{Domain: domain}, nil)
}

func (c *Client) DeleteDNSZone(ctx context.Context, zoneID int64) error {
	return c.Delete(ctx, APICore, fmt.Sprintf("/dnszone/%d", zoneID), nil)
}

func (c *Client) UpdateDNSZone(ctx context.Context, zoneID int64, model UpdateDNSZoneModel) error {
	return c.Post(ctx, APICore, fmt.Sprintf("/dnszone/%d", zoneID), model, nil)
}

func (c *Client) CheckDNSZoneAvailability(ctx context.Context, domain string) (bool, error) {
	var result struct {
		Available bool `json:"available"`
	}
	err := c.Post(ctx, APICore, "/dnszone/checkavailability",
		map[string]string{"domain": domain}, &result)
	if err != nil {
		return false, err
	}
	return result.Available, nil
}

func (c *Client) ListDNSRecords(ctx context.Context, zoneID int64) ([]DNSRecord, error) {
	var resp struct {
		Items []DNSRecord `json:"Items"`
	}
	err := c.Get(ctx, APICore, fmt.Sprintf("/dnszone/%d/records", zoneID), &resp)
	if err != nil {
		return nil, err
	}
	if resp.Items == nil {
		return []DNSRecord{}, nil
	}
	return resp.Items, nil
}

func (c *Client) AddDNSRecord(ctx context.Context, zoneID int64, record AddDNSRecordModel) (*DNSRecord, error) {
	var resp DNSRecord
	err := c.Put(ctx, APICore, fmt.Sprintf("/dnszone/%d/records", zoneID), record, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) UpdateDNSRecord(ctx context.Context, zoneID, recordID int64, record UpdateDNSRecordModel) error {
	return c.Post(ctx, APICore, fmt.Sprintf("/dnszone/%d/records/%d", zoneID, recordID), record, nil)
}

func (c *Client) DeleteDNSRecord(ctx context.Context, zoneID, recordID int64) error {
	return c.Delete(ctx, APICore, fmt.Sprintf("/dnszone/%d/records/%d", zoneID, recordID), nil)
}

func (c *Client) ExportDNSZone(ctx context.Context, zoneID int64) (string, error) {
	var data string
	err := c.Get(ctx, APICore, fmt.Sprintf("/dnszone/%d/export", zoneID), &data)
	if err != nil {
		return "", err
	}
	return data, nil
}

func (c *Client) ImportDNSRecords(ctx context.Context, zoneID int64) (*DNSZoneImportResult, error) {
	var result DNSZoneImportResult
	err := c.Post(ctx, APICore, fmt.Sprintf("/dnszone/%d/import", zoneID), nil, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) IssueWildcardCert(ctx context.Context, zoneID int64) error {
	return c.Post(ctx, APICore, fmt.Sprintf("/dnszone/%d/certificate/issue", zoneID), nil, nil)
}

var dnsTypeToName = map[int32]string{
	DNSRecordA:        "A",
	DNSRecordAAAA:     "AAAA",
	DNSRecordCNAME:    "CNAME",
	DNSRecordTXT:      "TXT",
	DNSRecordMX:       "MX",
	DNSRecordRedirect: "Redirect",
	DNSRecordFlatten:  "Flatten",
	DNSRecordPullZone: "PullZone",
	DNSRecordSRV:      "SRV",
	DNSRecordCAA:      "CAA",
	DNSRecordPTR:      "PTR",
	DNSRecordScript:   "Script",
	DNSRecordNS:       "NS",
	DNSRecordSVCB:     "SVCB",
	DNSRecordHTTPS:    "HTTPS",
	DNSRecordTLSA:     "TLSA",
}

var nameToDNSType = map[string]int32{}
var dnsNames []string

func init() {
	for k, v := range dnsTypeToName {
		nameToDNSType[v] = k
		dnsNames = append(dnsNames, v)
	}
}

func ParseDNSRecordType(s string) int32 {
	if t, ok := nameToDNSType[strings.ToUpper(s)]; ok {
		return t
	}
	n, err := strconv.Atoi(s)
	if err == nil {
		if n < math.MinInt32 || n > math.MaxInt32 {
			return DNSRecordA
		}
		return int32(n)
	}
	return DNSRecordA
}

func DNSRecordTypeName(t int32) string {
	if n, ok := dnsTypeToName[t]; ok {
		return n
	}
	return strconv.Itoa(int(t))
}
