package aws

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	elbv2types "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2/types"
	"github.com/aws/aws-sdk-go-v2/service/route53"
	route53types "github.com/aws/aws-sdk-go-v2/service/route53/types"

	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) CreateBareMetal(ctx context.Context, cfg providers.BareMetalCreateConfig) (*providers.BareMetal, error) {
	result, err := c.ec2Client.RunInstances(ctx, &ec2.RunInstancesInput{
		ImageId:      &cfg.Template,
		InstanceType: ec2types.InstanceType(cfg.Plan),
		MinCount:     aws.Int32(1),
		MaxCount:     aws.Int32(1),
		TagSpecifications: []ec2types.TagSpecification{{
			ResourceType: ec2types.ResourceTypeInstance,
			Tags:         []ec2types.Tag{{Key: aws.String("Name"), Value: &cfg.Hostname}},
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("aws create baremetal: %w", err)
	}
	if len(result.Instances) == 0 {
		return nil, fmt.Errorf("aws: no baremetal instances created")
	}
	inst := result.Instances[0]
	v := &providers.BareMetal{
		ID:   aws.ToString(inst.InstanceId),
		Name: cfg.Hostname,
	}
	if inst.PublicIpAddress != nil {
		v.IP = *inst.PublicIpAddress
	}
	return v, nil
}

func (c *Client) DeleteBareMetal(ctx context.Context, id string) error {
	_, err := c.ec2Client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: []string{id},
	})
	return err
}

func (c *Client) ListBareMetal(ctx context.Context) ([]providers.BareMetal, error) {
	result, err := c.ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{})
	if err != nil {
		return nil, fmt.Errorf("aws list baremetal: %w", err)
	}
	var list []providers.BareMetal
	for _, r := range result.Reservations {
		for _, inst := range r.Instances {
			bm := providers.BareMetal{ID: aws.ToString(inst.InstanceId), Name: aws.ToString(inst.InstanceId)}
			if inst.PublicIpAddress != nil {
				bm.IP = *inst.PublicIpAddress
			}
			list = append(list, bm)
		}
	}
	return list, nil
}

func (c *Client) CreateLB(ctx context.Context, cfg providers.LBCreateConfig) (*providers.LoadBalancer, error) {
	lb, err := c.elbv2Client.CreateLoadBalancer(ctx, &elasticloadbalancingv2.CreateLoadBalancerInput{
		Name:   &cfg.Name,
		Scheme: elbv2types.LoadBalancerSchemeEnumInternetFacing,
		Type:   elbv2types.LoadBalancerTypeEnumApplication,
		Tags:   []elbv2types.Tag{{Key: aws.String("Name"), Value: &cfg.Label}},
	})
	if err != nil {
		return nil, fmt.Errorf("aws create lb: %w", err)
	}
	return &providers.LoadBalancer{
		ID:   aws.ToString(lb.LoadBalancers[0].LoadBalancerArn),
		Name: cfg.Name,
		IP:   aws.ToString(lb.LoadBalancers[0].DNSName),
	}, nil
}

func (c *Client) DeleteLB(ctx context.Context, id string) error {
	_, err := c.elbv2Client.DeleteLoadBalancer(ctx, &elasticloadbalancingv2.DeleteLoadBalancerInput{
		LoadBalancerArn: &id,
	})
	return err
}

func (c *Client) ListLB(ctx context.Context) ([]providers.LoadBalancer, error) {
	lbs, err := c.elbv2Client.DescribeLoadBalancers(ctx, &elasticloadbalancingv2.DescribeLoadBalancersInput{})
	if err != nil {
		return nil, fmt.Errorf("aws list lb: %w", err)
	}
	var result []providers.LoadBalancer
	for _, lb := range lbs.LoadBalancers {
		result = append(result, providers.LoadBalancer{
			ID: aws.ToString(lb.LoadBalancerArn), Name: aws.ToString(lb.LoadBalancerName),
			IP: aws.ToString(lb.DNSName),
		})
	}
	return result, nil
}

func (c *Client) ListDNSZones(ctx context.Context) ([]providers.DNSZone, error) {
	zones, err := c.route53Client.ListHostedZones(ctx, &route53.ListHostedZonesInput{})
	if err != nil {
		return nil, fmt.Errorf("aws list hosted zones: %w", err)
	}
	var result []providers.DNSZone
	for _, z := range zones.HostedZones {
		result = append(result, providers.DNSZone{
			ID:   aws.ToString(z.Id),
			Name: aws.ToString(z.Name),
		})
	}
	return result, nil
}

func (c *Client) CreateDNSRecord(ctx context.Context, zoneID string, r providers.DNSRecord) error {
	ttl := int64(r.TTL)
	if ttl == 0 {
		ttl = 300
	}
	_, err := c.route53Client.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
		HostedZoneId: &zoneID,
		ChangeBatch: &route53types.ChangeBatch{
			Changes: []route53types.Change{{
				Action: route53types.ChangeActionCreate,
				ResourceRecordSet: &route53types.ResourceRecordSet{
					Name: &r.Name,
					Type: route53types.RRType(r.Type),
					TTL:  aws.Int64(ttl),
					ResourceRecords: []route53types.ResourceRecord{{
						Value: &r.Value,
					}},
				},
			}},
		},
	})
	return err
}

func (c *Client) DeleteDNSRecord(ctx context.Context, zoneID, recordID string) error {
	// recordID is the record Name. We list records to find the matching entry.
	records, err := c.route53Client.ListResourceRecordSets(ctx, &route53.ListResourceRecordSetsInput{
		HostedZoneId: &zoneID,
		MaxItems:     aws.Int32(300),
	})
	if err != nil {
		return fmt.Errorf("aws route53 list records: %w", err)
	}
	for _, r := range records.ResourceRecordSets {
		name := aws.ToString(r.Name)
		name = strings.TrimSuffix(name, ".")
		id := strings.TrimSuffix(recordID, ".")
		if name != id {
			continue
		}
		_, err := c.route53Client.ChangeResourceRecordSets(ctx, &route53.ChangeResourceRecordSetsInput{
			HostedZoneId: &zoneID,
			ChangeBatch: &route53types.ChangeBatch{
				Changes: []route53types.Change{{
					Action:            route53types.ChangeActionDelete,
					ResourceRecordSet: &r,
				}},
			},
		})
		if err != nil {
			return fmt.Errorf("aws route53 delete record %s: %w", name, err)
		}
		return nil
	}
	return fmt.Errorf("aws route53: record %s not found in zone %s", recordID, zoneID)
}

func (c *Client) CreateLBListener(ctx context.Context, lbID string, cfg providers.LBListenerConfig) (*providers.LBListener, error) {
	return nil, fmt.Errorf("aws: method not available")
}

func (c *Client) UpdateLBListener(ctx context.Context, lbID, listenerID string, cfg providers.LBListenerConfig) (*providers.LBListener, error) {
	return nil, fmt.Errorf("aws: method not available")
}

func (c *Client) DeleteLBListener(ctx context.Context, lbID, listenerID string) error {
	return fmt.Errorf("aws: method not available")
}

func (c *Client) SetLBHealthCheck(ctx context.Context, lbID, listenerID string, cfg providers.LBHealthCheckConfig) error {
	return fmt.Errorf("aws: method not available")
}

func (c *Client) AddLBTarget(ctx context.Context, lbID, listenerID string, cfg providers.LBTargetConfig) (*providers.LBTarget, error) {
	return nil, fmt.Errorf("aws: method not available")
}

func (c *Client) ListLBTargets(ctx context.Context, lbID, listenerID string) ([]providers.LBTarget, error) {
	return nil, fmt.Errorf("aws: method not available")
}

func (c *Client) DrainLBTarget(ctx context.Context, lbID, listenerID, targetID string) error {
	return fmt.Errorf("aws: method not available")
}

func (c *Client) ResizeLB(ctx context.Context, lbID, plan string) (*providers.LoadBalancer, error) {
	return nil, fmt.Errorf("aws: method not available")
}

func (c *Client) GetLBMetrics(ctx context.Context, lbID string) (string, error) {
	return "", fmt.Errorf("aws: method not available")
}

func (c *Client) ToggleLBProtection(ctx context.Context, lbID string) (*providers.LoadBalancer, error) {
	return nil, fmt.Errorf("aws: method not available")
}
