package aws

import (
	"context"
	"encoding/base64"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/ec2/types"

	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) CreateVPS(ctx context.Context, cfg providers.VPSCreateConfig) (*providers.VPS, error) {
	userData := ""
	if cfg.UserData != "" {
		userData = base64.StdEncoding.EncodeToString([]byte(cfg.UserData))
	}

	result, err := c.ec2Client.RunInstances(ctx, &ec2.RunInstancesInput{
		ImageId:      &cfg.Template,
		InstanceType: types.InstanceType(cfg.Plan),
		MinCount:     aws.Int32(1),
		MaxCount:     aws.Int32(1),
		UserData:     aws.String(userData),
		TagSpecifications: []types.TagSpecification{{
			ResourceType: types.ResourceTypeInstance,
			Tags:         []types.Tag{{Key: aws.String("Name"), Value: &cfg.Hostname}},
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("aws create instance: %w", err)
	}
	if len(result.Instances) == 0 {
		return nil, fmt.Errorf("aws: no instances created")
	}
	inst := result.Instances[0]
	v := &providers.VPS{
		ID:   aws.ToString(inst.InstanceId),
		Name: cfg.Hostname,
	}
	if inst.PublicIpAddress != nil {
		v.IP = *inst.PublicIpAddress
	}
	return v, nil
}

func (c *Client) DeleteVPS(ctx context.Context, id string) error {
	_, err := c.ec2Client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: []string{id},
	})
	return err
}

func (c *Client) ListVPS(ctx context.Context) ([]providers.VPS, error) {
	result, err := c.ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{})
	if err != nil {
		return nil, fmt.Errorf("aws list instances: %w", err)
	}
	var vpss []providers.VPS
	for _, r := range result.Reservations {
		for _, inst := range r.Instances {
			v := providers.VPS{ID: aws.ToString(inst.InstanceId)}
			if inst.PublicIpAddress != nil {
				v.IP = *inst.PublicIpAddress
			}
			vpss = append(vpss, v)
		}
	}
	return vpss, nil
}

func (c *Client) GetVPS(ctx context.Context, id string) (*providers.VPS, error) {
	result, err := c.ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
		InstanceIds: []string{id},
	})
	if err != nil {
		return nil, fmt.Errorf("aws get instance: %w", err)
	}
	if len(result.Reservations) > 0 && len(result.Reservations[0].Instances) > 0 {
		inst := result.Reservations[0].Instances[0]
		v := &providers.VPS{ID: aws.ToString(inst.InstanceId)}
		if inst.PublicIpAddress != nil {
			v.IP = *inst.PublicIpAddress
		}
		return v, nil
	}
	return nil, fmt.Errorf("aws instance %s not found", id)
}
