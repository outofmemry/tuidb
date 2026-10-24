// Package iam builds AWS RDS IAM authentication tokens, used as the database password.
package iam

import (
	"context"
	"fmt"
	"net"
	"strconv"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/rds/auth"

	"webtuidb/internal/config"
)

// Token returns a token valid for 15 minutes. Credentials come from the default AWS chain; the
// region from the connection's "region" option, else the AWS config.
func Token(ctx context.Context, c config.Connection) (string, error) {
	switch c.Driver { // the RDS signer: a token for any other engine cannot authenticate
	case "", config.Postgres, config.MySQL, config.MariaDB:
	default:
		return "", fmt.Errorf("iam_auth is AWS RDS IAM authentication and is not available for %s", c.Driver)
	}
	cfg, err := awsconfig.LoadDefaultConfig(ctx)
	if err != nil {
		return "", fmt.Errorf("iam_auth: %w", err)
	}
	region := c.Options["region"]
	if region == "" {
		region = cfg.Region
	}
	if region == "" {
		return "", fmt.Errorf("iam_auth: no AWS region (set option region=... or AWS_REGION)")
	}
	endpoint := net.JoinHostPort(c.Host, strconv.Itoa(c.EffectivePort()))
	tok, err := auth.BuildAuthToken(ctx, endpoint, region, c.User, cfg.Credentials)
	if err != nil {
		return "", fmt.Errorf("iam_auth: %w", err)
	}
	return tok, nil
}
