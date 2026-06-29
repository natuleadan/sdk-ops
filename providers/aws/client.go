package aws

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/elasticloadbalancingv2"
	"github.com/aws/aws-sdk-go-v2/service/route53"
)

type Client struct {
	ec2Client     *ec2.Client
	eksClient     *eks.Client
	elbv2Client   *elasticloadbalancingv2.Client
	route53Client *route53.Client
	region        string
}

func New(region string, cfg aws.Config) *Client {
	return &Client{
		ec2Client:     ec2.NewFromConfig(cfg),
		eksClient:     eks.NewFromConfig(cfg),
		elbv2Client:   elasticloadbalancingv2.NewFromConfig(cfg),
		route53Client: route53.NewFromConfig(cfg),
		region:        region,
	}
}
