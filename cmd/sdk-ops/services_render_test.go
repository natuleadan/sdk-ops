package main

import "testing"

func TestBareS3Host(t *testing.T) {
	cases := map[string]string{
		"https://s3.example.com":  "s3.example.com",
		"http://s3.example.com":   "s3.example.com",
		"s3.example.com":          "s3.example.com",
		"https://s3.example.com/": "s3.example.com",
		"s3.example.com/":         "s3.example.com",
		"":                        "",
	}
	for in, want := range cases {
		if got := bareS3Host(in); got != want {
			t.Errorf("bareS3Host(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClusterS3PrefixAndEndpointNormalization(t *testing.T) {
	t.Setenv("S3_ENDPOINT", "https://s3.example.com/")
	t.Setenv("S3_BUCKET", "bucket")
	t.Setenv("S3_PREFIX", "leaked-from-another-service")
	t.Setenv("DF_S3_PREFIX", "")
	t.Setenv("PG_S3_PREFIX", "")

	prof := map[string]any{
		"cpu": "100m", "mem": "128Mi",
		"CPU": "100m", "Cpus": "100m", "Mem": "128Mi", "MemLimit": "256Mi",
		"MaxConnections": "100", "StorageSize": "1Gi",
	}

	df, err := dfClusterRenderData(prof)
	if err != nil {
		t.Fatal(err)
	}
	if df["S3Endpoint"] != "s3.example.com" || df["S3Prefix"] != "df" {
		t.Errorf("df render: endpoint=%v prefix=%v (shared S3_PREFIX must not leak)",
			df["S3Endpoint"], df["S3Prefix"])
	}

	pg, err := pgsqlCNPGRenderData(prof)
	if err != nil {
		t.Fatal(err)
	}
	if pg["S3Endpoint"] != "s3.example.com" || pg["S3Prefix"] != "pg" {
		t.Errorf("pg render: endpoint=%v prefix=%v (endpointURL template composes https://)",
			pg["S3Endpoint"], pg["S3Prefix"])
	}

	t.Setenv("DF_S3_PREFIX", "dfx")
	t.Setenv("PG_S3_PREFIX", "pgx")
	df, err = dfClusterRenderData(prof)
	if err != nil {
		t.Fatal(err)
	}
	pg, err = pgsqlCNPGRenderData(prof)
	if err != nil {
		t.Fatal(err)
	}
	if df["S3Prefix"] != "dfx" || pg["S3Prefix"] != "pgx" {
		t.Errorf("per-service overrides not honored: df=%v pg=%v", df["S3Prefix"], pg["S3Prefix"])
	}

	if got := s3PrefixOr("VK_S3_PREFIX", "valkey"); got != "valkey" {
		t.Errorf("valkey default prefix = %q, want valkey", got)
	}
	if got := s3PrefixOr("NATS_S3_PREFIX", "nats"); got != "nats" {
		t.Errorf("nats default prefix = %q, want nats", got)
	}
	t.Setenv("VK_S3_PREFIX", "vkx")
	if got := s3PrefixOr("VK_S3_PREFIX", "valkey"); got != "vkx" {
		t.Errorf("valkey override prefix = %q, want vkx", got)
	}
}
