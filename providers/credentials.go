package providers

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Credentials struct {
	CubePathAPIKey     string `yaml:"cubepath_api_key"`
	HetznerAPIToken    string `yaml:"hetzner_api_token"`
	DigitalOceanToken  string `yaml:"digitalocean_token"`
	VultrAPIKey        string `yaml:"vultr_api_key"`
	BunnyAPIKey        string `yaml:"bunny_api_key"`
	CivoAPIKey         string `yaml:"civo_api_key"`
	AWSRegion          string `yaml:"aws_region"`
	AWSProfile         string `yaml:"aws_profile"`
}

func CredentialsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".sdk-ops", "credentials.yaml")
}

func LoadCredentials() (*Credentials, error) {
	path := filepath.Clean(CredentialsPath())
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Credentials{}, nil
		}
		return nil, err
	}

	var c Credentials
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

func LoadCredentialsFromEnv() *Credentials {
	return &Credentials{
		HetznerAPIToken:   os.Getenv("HETZNER_API_TOKEN"),
		DigitalOceanToken: os.Getenv("DIGITALOCEAN_TOKEN"),
		VultrAPIKey:       os.Getenv("VULTR_API_KEY"),
		BunnyAPIKey:       os.Getenv("BUNNY_API_KEY"),
		CivoAPIKey:        os.Getenv("CIVO_API_KEY"),
		AWSRegion:         os.Getenv("AWS_REGION"),
		AWSProfile:        os.Getenv("AWS_PROFILE"),
	}
}

func SaveCredentials(c *Credentials) error {
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}

	dir := filepath.Dir(CredentialsPath())
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	return os.WriteFile(CredentialsPath(), data, 0600)
}
