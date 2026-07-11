package bunny

import "context"

type DeployOptions struct {
	AppName    string
	Runtime    ApplicationRuntimeType
	MinInstances int32
	MaxInstances int32
	Regions    []string
	Image      string // full image ref: ghcr.io/user/repo:tag
	Port       int32
	Env        map[string]string
	VolumeName string
	VolumeSize int32
	VolumePath string
	RegistryID string // optional, overrides auto-detection
	Anycast    bool   // use Anycast IP instead of CDN endpoint
}

func (c *Client) DeployFromImage(ctx context.Context, opts DeployOptions) (*AddApplicationResponse, error) {
	if opts.MinInstances == 0 {
		opts.MinInstances = 1
	}
	if opts.MaxInstances == 0 {
		opts.MaxInstances = 1
	}
	if opts.Runtime == "" {
		opts.Runtime = RuntimeShared
	}
	if len(opts.Regions) == 0 {
		opts.Regions = []string{"CO"}
	}

	namespace, name, tag := parseImageRef(opts.Image)

	policy := PullIfNotPresent
	registryID := opts.RegistryID
	if registryID == "" {
		registryID = detectRegistryID(opts.Image)
	}

	req := AddApplicationRequest{
		Name:        opts.AppName,
		RuntimeType: opts.Runtime,
		AutoScaling: AutoscalingSettings{
			Min: opts.MinInstances,
			Max: opts.MaxInstances,
		},
		RegionSettings: UpdateRegionSettingsRequest{
			AllowedRegionIDs:  opts.Regions,
			RequiredRegionIDs: opts.Regions,
		},
		ContainerTemplates: []ContainerRequest{
			{
				Name:            "app",
				ImageName:       name,
				ImageNamespace:  namespace,
				ImageTag:        tag,
				ImageRegistryID: registryID,
				ImagePullPolicy: &policy,
				EnvironmentVariables: envMapToSlice(opts.Env),
				Endpoints: []EndpointRequest{
					buildEndpoint(opts.Port, opts.Anycast),
				},
			},
		},
	}

	if opts.VolumeName != "" && opts.VolumePath != "" {
		if opts.VolumeSize == 0 {
			opts.VolumeSize = 5
		}
		req.Volumes = []VolumeRequest{
			{Name: opts.VolumeName, Size: opts.VolumeSize},
		}
		req.ContainerTemplates[0].VolumeMounts = []VolumeMountRequest{
			{Name: opts.VolumeName, MountPath: opts.VolumePath},
		}
	}

	return c.CreateApp(ctx, req)
}

func parseImageRef(ref string) (namespace, name, tag string) {
	tag = "latest"
	// parse registry/repo:tag
	// ghcr.io/user/repo:tag
	// docker.io/library/redis:7-alpine
	var repo string
	for i := len(ref) - 1; i >= 0; i-- {
		if ref[i] == ':' && i > 0 && ref[i-1] != '/' {
			tag = ref[i+1:]
			repo = ref[:i]
			break
		}
	}
	if repo == "" {
		repo = ref
	}

	parts := splitPath(repo)
	switch len(parts) {
	case 1:
		namespace = "library"
		name = parts[0]
	case 2:
		namespace = parts[0]
		name = parts[1]
	case 3:
		namespace = parts[1]
		name = parts[2]
	default:
		namespace = parts[len(parts)-2]
		name = parts[len(parts)-1]
	}
	return
}

func splitPath(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '/' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}

func envMapToSlice(env map[string]string) []EnvironmentVariable {
	if env == nil {
		return nil
	}
	var vars []EnvironmentVariable
	for k, v := range env {
		vars = append(vars, EnvironmentVariable{Name: k, Value: v})
	}
	return vars
}

func buildEndpoint(port int32, anycast bool) EndpointRequest {
	if anycast {
		proto := AnycastIPv4
		return EndpointRequest{
			DisplayName: "HTTP-Anycast",
			Anycast: &AnycastEndpointRequest{
				Type: proto,
				PortMappings: []ContainerPortMappingRequest{
					{
						ContainerPort: port,
						ExposedPort:   &port,
						Protocols:     []Protocol{ProtoTCP},
					},
				},
			},
		}
	}
	return EndpointRequest{
		DisplayName: "HTTP",
		CDN: &CDNEndpointRequest{
			IsSSLEnabled: true,
			PortMappings: []ContainerPortMappingRequest{
				{
					ContainerPort: port,
					Protocols:     []Protocol{ProtoTCP},
				},
			},
		},
	}
}

func detectRegistryID(image string) string {
	if len(image) > 7 && image[:8] == "ghcr.io/" {
		return "1156"
	}
	return "1155"
}
