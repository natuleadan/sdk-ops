package aws

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ec2"

	"github.com/natuleadan/sdk-ops/providers"
)

func (c *Client) CreateSSHKey(ctx context.Context, cfg providers.SSHKeyCreateConfig) (*providers.SSHKey, error) {
	result, err := c.ec2Client.ImportKeyPair(ctx, &ec2.ImportKeyPairInput{
		KeyName:           &cfg.Name,
		PublicKeyMaterial: []byte(cfg.PublicKey),
	})
	if err != nil {
		return nil, fmt.Errorf("aws import key pair: %w", err)
	}
	return &providers.SSHKey{
		ID:          aws.ToString(result.KeyPairId),
		Name:        aws.ToString(result.KeyName),
		Fingerprint: aws.ToString(result.KeyFingerprint),
	}, nil
}

func (c *Client) ListSSHKeys(ctx context.Context) ([]providers.SSHKey, error) {
	result, err := c.ec2Client.DescribeKeyPairs(ctx, &ec2.DescribeKeyPairsInput{})
	if err != nil {
		return nil, fmt.Errorf("aws list key pairs: %w", err)
	}
	var out []providers.SSHKey
	for _, k := range result.KeyPairs {
		out = append(out, providers.SSHKey{
			ID: aws.ToString(k.KeyPairId), Name: aws.ToString(k.KeyName),
			Fingerprint: aws.ToString(k.KeyFingerprint),
		})
	}
	return out, nil
}

func (c *Client) DeleteSSHKey(ctx context.Context, id string) error {
	_, err := c.ec2Client.DeleteKeyPair(ctx, &ec2.DeleteKeyPairInput{KeyPairId: &id})
	return err
}
