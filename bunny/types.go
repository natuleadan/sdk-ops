package bunny

// --- API Routing ---
const (
	APICore    = "core"
	APIMC      = "mc"
	APILogging = "logging"
	APIVideo   = "video"
	APIShield  = "shield"
	APIStorage = "storage"
)

// --- Pagination ---

type ListMeta struct {
	TotalItems int64 `json:"totalItems,omitempty"`
}

// --- Error ---

type ErrorDetails struct {
	Title    string            `json:"title"`
	Status   int32             `json:"status"`
	Detail   *string           `json:"detail,omitempty"`
	Errors   []ValidationError `json:"errors,omitempty"`
}

type ValidationError struct {
	Field   *string `json:"field,omitempty"`
	Message string  `json:"message"`
}

// ============================================================================
// Magic Containers Types
// ============================================================================

type ApplicationRuntimeType string

const (
	RuntimeShared   ApplicationRuntimeType = "shared"
	RuntimeReserved ApplicationRuntimeType = "reserved"
)

type ApplicationStatus string

const (
	AppStatusUnknown     ApplicationStatus = "unknown"
	AppStatusActive      ApplicationStatus = "active"
	AppStatusProgressing ApplicationStatus = "progressing"
	AppStatusInactive    ApplicationStatus = "inactive"
	AppStatusFailing     ApplicationStatus = "failing"
	AppStatusSuspended   ApplicationStatus = "suspended"
)

type EndpointType string

const (
	EndpointCDN      EndpointType = "cdn"
	EndpointAnycast  EndpointType = "anycast"
	EndpointPublicIP EndpointType = "publicIp"
)

type ImagePullPolicy string

const (
	PullAlways      ImagePullPolicy = "always"
	PullIfNotPresent ImagePullPolicy = "ifNotPresent"
)

type Protocol string

const (
	ProtoTCP  Protocol = "tcp"
	ProtoUDP  Protocol = "udp"
	ProtoSCTP Protocol = "sctp"
)

type VolumeStatus string

const (
	VolUnknown    VolumeStatus = "unknown"
	VolAttached   VolumeStatus = "attached"
	VolDetached   VolumeStatus = "detached"
	VolExtending  VolumeStatus = "extending"
	VolDeleting   VolumeStatus = "deleting"
	VolCreating   VolumeStatus = "creating"
	VolNotSched   VolumeStatus = "notScheduled"
	VolScheduled  VolumeStatus = "scheduled"
	VolFailed     VolumeStatus = "failed"
)

type RegistryType string

const (
	RegDockerHub RegistryType = "dockerHub"
	RegGitHub    RegistryType = "gitHub"
)

type DataGranularity string

const (
	GranularityDaily  DataGranularity = "Daily"
	GranularityHourly DataGranularity = "Hourly"
	GranularityMinute DataGranularity = "Minute"
)

// --- Application CRUD ---

type AddApplicationRequest struct {
	Name                         string                    `json:"name"`
	RuntimeType                  ApplicationRuntimeType     `json:"runtimeType"`
	TerminationGracePeriodSeconds *int64                    `json:"terminationGracePeriodSeconds,omitempty"`
	AutoScaling                  AutoscalingSettings        `json:"autoScaling"`
	RegionSettings               UpdateRegionSettingsRequest `json:"regionSettings"`
	ContainerTemplates           []ContainerRequest          `json:"containerTemplates,omitempty"`
	Volumes                      []VolumeRequest             `json:"volumes,omitempty"`
}

type AddApplicationResponse struct {
	ID string `json:"id"`
}

type Application struct {
	ID                 string               `json:"id"`
	Name               string               `json:"name"`
	DisplayEndpoint    *DisplayEndpoint     `json:"displayEndpoint,omitempty"`
	Status             ApplicationStatus    `json:"status"`
	RuntimeType        ApplicationRuntimeType `json:"runtimeType"`
	RegionSettings     RegionSettings       `json:"regionSettings"`
	ContainerTemplates []ContainerTemplate  `json:"containerTemplates"`
	ContainerInstances []ContainerInstance  `json:"containerInstances"`
	Volumes            []VolumeTemplate     `json:"volumes"`
	AutoScaling        *AutoscalingSettings `json:"autoScaling,omitempty"`
	NetworkSettings    *NetworkLimits       `json:"networkSettings,omitempty"`
}

type AppListItem struct {
	ID              string            `json:"id"`
	Name            string            `json:"name"`
	Description     string            `json:"description,omitempty"`
	DisplayEndpoint *DisplayEndpoint  `json:"displayEndpoint,omitempty"`
	Status          ApplicationStatus `json:"status"`
}

type ListApplicationsResponse struct {
	Items  []AppListItem `json:"items,omitempty"`
	Meta   *ListMeta     `json:"meta,omitempty"`
	Cursor *string       `json:"cursor,omitempty"`
}

type PatchApplicationRequest struct {
	Name                         *string                     `json:"name,omitempty"`
	RuntimeType                  *ApplicationRuntimeType     `json:"runtimeType,omitempty"`
	AutoScaling                  *AutoscalingSettings        `json:"autoScaling,omitempty"`
	RegionSettings               *UpdateRegionSettingsRequest `json:"regionSettings,omitempty"`
	ContainerTemplates           *[]ContainerRequest          `json:"containerTemplates,omitempty"`
}

// --- Containers ---

type ContainerRequest struct {
	ID                   *string                `json:"id,omitempty"`
	Name                 string                 `json:"name"`
	Image                *string                `json:"image,omitempty"`
	ImageName            string                 `json:"imageName"`
	ImageNamespace       string                 `json:"imageNamespace"`
	ImageTag             string                 `json:"imageTag"`
	ImageDigest          *string                `json:"imageDigest,omitempty"`
	ImageRegistryID      string                 `json:"imageRegistryId"`
	ImagePullPolicy      *ImagePullPolicy       `json:"imagePullPolicy,omitempty"`
	EntryPoint           *ContainerEntryPoint   `json:"entryPoint,omitempty"`
	Probes               *ContainerProbes       `json:"probes,omitempty"`
	EnvironmentVariables []EnvironmentVariable  `json:"environmentVariables,omitempty"`
	Endpoints            []EndpointRequest      `json:"endpoints,omitempty"`
	VolumeMounts         []VolumeMountRequest   `json:"volumeMounts,omitempty"`
}

type ContainerTemplate struct {
	ID                   string                `json:"id"`
	Name                 string                `json:"name"`
	PackageID            string                `json:"packageId"`
	Image                string                `json:"image"`
	ImageName            string                `json:"imageName"`
	ImageNamespace       string                `json:"imageNamespace"`
	ImageTag             string                `json:"imageTag"`
	ImageRegistryID      string                `json:"imageRegistryId"`
	ImageDigest          string                `json:"imageDigest"`
	ImagePullPolicy      ImagePullPolicy       `json:"imagePullPolicy"`
	EntryPoint           ContainerEntryPoint   `json:"entryPoint"`
	Probes               ContainerProbes       `json:"probes"`
	EnvironmentVariables []EnvironmentVariable `json:"environmentVariables"`
	Endpoints            []ContainerEndpoint   `json:"endpoints"`
	VolumeMounts         []ContainerVolumeMount `json:"volumeMounts"`
}

type ContainerInstance struct {
	ID           string `json:"id"`
	TemplateID   string `json:"templateId"`
	PodID        string `json:"podId"`
	NodeIPAddress string `json:"nodeIpAddress"`
}

type EnvironmentVariable struct {
	Name  string `json:"name"`
	Value string `json:"value,omitempty"`
}

type ContainerEntryPoint struct {
	Command         *string  `json:"command,omitempty"`
	CommandArray    []string `json:"commandArray,omitempty"`
	Arguments       *string  `json:"arguments,omitempty"`
	ArgumentsArray  []string `json:"argumentsArray,omitempty"`
	WorkingDirectory *string `json:"workingDirectory,omitempty"`
}

type ContainerProbes struct {
	Startup   *ContainerProbe `json:"startup,omitempty"`
	Readiness *ContainerProbe `json:"readiness,omitempty"`
	Liveness  *ContainerProbe `json:"liveness,omitempty"`
}

type ContainerProbe struct {
	InitialDelaySeconds *int32              `json:"initialDelaySeconds,omitempty"`
	PeriodSeconds       *int32              `json:"periodSeconds,omitempty"`
	TimeoutSeconds      *int32              `json:"timeoutSeconds,omitempty"`
	FailureThreshold    *int32              `json:"failureThreshold,omitempty"`
	SuccessThreshold    *int32              `json:"successThreshold,omitempty"`
	HTTPGet             *HTTPGetProbe       `json:"httpGet,omitempty"`
	TCPSocket           *TCPSocketProbe     `json:"tcpSocket,omitempty"`
}

type HTTPGetProbe struct {
	Request  *HTTPGetProbeRequest  `json:"request,omitempty"`
	Response *HTTPGetProbeResponse `json:"response,omitempty"`
}

type HTTPGetProbeRequest struct {
	Path       string `json:"path,omitempty"`
	PortNumber int32  `json:"portNumber,omitempty"`
}

type HTTPGetProbeResponse struct {
	ExpectedStatusCode string `json:"expectedStatusCode,omitempty"`
}

type TCPSocketProbe struct {
	Request *TCPSocketProbeRequest `json:"request,omitempty"`
}

type TCPSocketProbeRequest struct {
	PortNumber int32 `json:"portNumber,omitempty"`
}

// --- Endpoints ---

type EndpointRequest struct {
	ID           *string               `json:"id,omitempty"`
	DisplayName  string                `json:"displayName"`
	CDN          *CDNEndpointRequest   `json:"cdn,omitempty"`
	Anycast      *AnycastEndpointRequest `json:"anycast,omitempty"`
}

type CDNEndpointRequest struct {
	IsSSLEnabled  bool                         `json:"isSslEnabled,omitempty"`
	StickySessions *StickySessionSettings      `json:"stickySessions,omitempty"`
	PullZoneID    *int32                       `json:"pullZoneId,omitempty"`
	PortMappings  []ContainerPortMappingRequest `json:"portMappings,omitempty"`
}

type AnycastEndpointRequest struct {
	Type         AnycastIPProtocolVersion      `json:"type"`
	PortMappings []ContainerPortMappingRequest `json:"portMappings"`
}

type AnycastIPProtocolVersion string

const (
	AnycastIPv4 AnycastIPProtocolVersion = "iPv4"
)

type ContainerPortMappingRequest struct {
	ContainerPort int32      `json:"containerPort"`
	ExposedPort   *int32     `json:"exposedPort,omitempty"`
	Protocols     []Protocol `json:"protocols,omitempty"`
}

type ContainerEndpoint struct {
	ID                string                 `json:"id"`
	DisplayName       string                 `json:"displayName"`
	PublicHost        string                 `json:"publicHost"`
	Type              EndpointType           `json:"type"`
	IsSSLEnabled      bool                   `json:"isSslEnabled"`
	PullZoneID        string                 `json:"pullZoneId"`
	PortMappings      []EndpointPortMapping  `json:"portMappings"`
	StickySessions    *StickySessionSettings `json:"stickySessions,omitempty"`
}

type EndpointPortMapping struct {
	ContainerPort int32        `json:"containerPort"`
	ExposedPort   int32        `json:"exposedPort"`
	Protocols     []Protocol   `json:"protocols"`
}

type EndpointListItem struct {
	ID          string                 `json:"id"`
	DisplayName string                 `json:"displayName"`
	PublicHost  string                 `json:"publicHost"`
	Type        EndpointType           `json:"type"`
	IsSSLEnabled bool                  `json:"isSslEnabled"`
	PullZoneID  string                 `json:"pullZoneId"`
	PortMappings []EndpointPortMapping `json:"portMappings"`
	ContainerName string               `json:"containerName"`
	ContainerID  string                `json:"containerId"`
	StickySessions *StickySessionSettings `json:"stickySessions,omitempty"`
	InternalIPAddresses []EndpointInternalIP `json:"internalIpAddresses,omitempty"`
	PublicIPAddresses   []EndpointInternalIP `json:"publicIpAddresses,omitempty"`
}

type StickySessionSettings struct {
	Enabled        bool     `json:"enabled"`
	SessionHeaders []string `json:"sessionHeaders"`
	CookieName     string   `json:"cookieName"`
}

type EndpointInternalIP struct {
	Address string `json:"address"`
	Region  string `json:"region"`
}

type DisplayEndpoint struct {
	ID      string       `json:"id"`
	Address string       `json:"address"`
	Type    EndpointType `json:"type"`
}

type ListEndpointsResponse struct {
	Items  []EndpointListItem `json:"items,omitempty"`
	Meta   *ListMeta          `json:"meta,omitempty"`
	Cursor *string            `json:"cursor,omitempty"`
}

type SaveEndpointResponse struct {
	ID string `json:"id"`
}

// --- Volumes ---

type VolumeRequest struct {
	Name string `json:"name"`
	Size int32  `json:"size"`
}

type VolumeMountRequest struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
}

type ContainerVolumeMount struct {
	Name      string `json:"name"`
	MountPath string `json:"mountPath"`
}

type VolumeTemplate struct {
	Name string  `json:"name"`
	Size float64 `json:"size"`
}

type VolumeInstance struct {
	ID                string       `json:"id"`
	AttachedPods      []string     `json:"attachedPods,omitempty"`
	AttachedContainers []string    `json:"attachedContainers,omitempty"`
	Region            string       `json:"region"`
	Status            VolumeStatus `json:"status"`
	Size              float64      `json:"size"`
	Usage             float64      `json:"usage"`
}

type VolumeInList struct {
	Name                  string           `json:"name"`
	ID                    string           `json:"id"`
	Size                  float64          `json:"size"`
	TotalUsage            float64          `json:"totalUsage"`
	TotalInstancesCount   int32            `json:"totalInstancesCount"`
	AttachedInstancesCount int32           `json:"attachedInstancesCount"`
	ContainersCount       int32            `json:"containersCount"`
	VolumeInstances       []VolumeInstance `json:"volumeInstances,omitempty"`
}

type ListVolumesResponse struct {
	Items  []VolumeInList      `json:"items,omitempty"`
	Meta   *ListMeta           `json:"meta,omitempty"`
	Cursor *string             `json:"cursor,omitempty"`
	Summary *ListVolumesSummary `json:"summary,omitempty"`
}

type ListVolumesSummary struct {
	TotalPods       int32   `json:"totalPods,omitempty"`
	TotalContainers int32   `json:"totalContainers,omitempty"`
	TotalStorage    float64 `json:"totalStorage,omitempty"`
}

// --- Autoscaling ---

type AutoscalingSettings struct {
	Min int32 `json:"min"`
	Max int32 `json:"max"`
}

// --- Regions ---

type Region struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Group             string `json:"group"`
	HasAnycastSupport bool   `json:"hasAnycastSupport"`
	HasCapacity       bool   `json:"hasCapacity"`
}

type RegionSettings struct {
	AllowedRegionIDs  []string `json:"allowedRegionIds"`
	RequiredRegionIDs []string `json:"requiredRegionIds"`
	MaxAllowedRegions *int32   `json:"maxAllowedRegions,omitempty"`
}

type UpdateRegionSettingsRequest struct {
	AllowedRegionIDs  []string          `json:"allowedRegionIds,omitempty"`
	RequiredRegionIDs []string          `json:"requiredRegionIds,omitempty"`
	MaxAllowedRegions *int32            `json:"maxAllowedRegions,omitempty"`
	NodeSelectors     map[string]string `json:"nodeSelectors,omitempty"`
}

type ListRegionsResponse struct {
	Items  []Region  `json:"items,omitempty"`
	Meta   *ListMeta `json:"meta,omitempty"`
	Cursor *string   `json:"cursor,omitempty"`
}

type OptimalBaseRegionResponse struct {
	Region *Region `json:"region,omitempty"`
}

// --- Container Registries ---

type ContainerRegistry struct {
	ID                   int64   `json:"id"`
	DisplayName          string  `json:"displayName"`
	HostName             string  `json:"hostName"`
	UserName             *string `json:"userName,omitempty"`
	FirstPasswordSymbols *string `json:"firstPasswordSymbols,omitempty"`
	CreatedAt            string  `json:"createdAt"`
	IsPublic             *bool   `json:"isPublic,omitempty"`
}

type ContainerRegistryRequest struct {
	DisplayName        string       `json:"displayName"`
	Type               *RegistryType `json:"type,omitempty"`
	PasswordCredentials *Credentials `json:"passwordCredentials,omitempty"`
}

type Credentials struct {
	UserName string `json:"userName"`
	Password string `json:"password"`
}

type ListContainerRegistriesResponse struct {
	Items  []ContainerRegistry `json:"items,omitempty"`
	Meta   *ListMeta           `json:"meta,omitempty"`
	Cursor *string             `json:"cursor,omitempty"`
}

type SaveContainerRegistryResult struct {
	ID     *int64  `json:"id,omitempty"`
	Error  *string `json:"error,omitempty"`
	Status string  `json:"status"`
}

type RemoveContainerRegistryResult struct {
	Status       string   `json:"status"`
	Applications []string `json:"applications,omitempty"`
}

type SearchPublicContainerImagesRequest struct {
	RegistryID string `json:"registryId"`
	Prefix     string `json:"prefix"`
	Size       *int32 `json:"size,omitempty"`
	Page       *int32 `json:"page,omitempty"`
}

type ContainerImage struct {
	ID        string `json:"id,omitempty"`
	Namespace string `json:"namespace,omitempty"`
}

type ContainerImageTag struct {
	Name string `json:"name,omitempty"`
}

type ImageConfig struct {
	EndpointSuggestions []EndpointRequest `json:"endpointSuggestions,omitempty"`
	VolumeSuggestions   []VolumeSuggestion `json:"volumeSuggestions,omitempty"`
}

type VolumeSuggestion struct {
	ContainerPath       string `json:"containerPath,omitempty"`
	SuggestedVolumeName string `json:"suggestedVolumeName,omitempty"`
}

type ImageTagInfo struct {
	ImageNamespace string `json:"imageNamespace,omitempty"`
	Image          string `json:"image,omitempty"`
	Tag            string `json:"tag,omitempty"`
	Digest         string `json:"digest,omitempty"`
}

// --- Network ---

type NetworkLimits struct {
	IngressBandwidthLimit int64 `json:"ingressBandwidthLimit,omitempty"`
	EgressBandwidthLimit  int64 `json:"egressBandwidthLimit,omitempty"`
}

// --- Overview & Stats ---

type Overview struct {
	TargetLatency       *DoubleStatusIndicator `json:"targetLatency,omitempty"`
	CurrentLatency      *DoubleStatusIndicator `json:"currentLatency,omitempty"`
	ActiveRegions       *Int32StatusIndicator  `json:"activeRegions,omitempty"`
	ActiveInstances     *Int32StatusIndicator  `json:"activeInstances,omitempty"`
	DesiredInstances    int32                  `json:"desiredInstances,omitempty"`
	Status              ApplicationStatus      `json:"status,omitempty"`
	AverageCPU          *DoubleStatusIndicator `json:"averageCPU,omitempty"`
	AverageRAM          *DoubleStatusIndicator `json:"averageRAM,omitempty"`
	AverageVolumesUsage *DoubleStatusIndicator `json:"averageVolumesUsage,omitempty"`
	Regions             []OverviewRegion       `json:"regions,omitempty"`
	AverageLatency      float64                `json:"averageLatency,omitempty"`
	TotalVolumeSizeInGB float64                `json:"totalVolumeSizeInGb,omitempty"`
	MonthlyCost         float64                `json:"monthlyCost,omitempty"`
	LatencyChart        map[string]float64     `json:"latencyChart,omitempty"`
}

type OverviewRegion struct {
	Region                      string         `json:"region"`
	IsRequired                  bool           `json:"isRequired"`
	Instances                   int32          `json:"instances"`
	Status                      string         `json:"status"`
	AverageCPU                  float64        `json:"averageCPU"`
	AverageRAM                  float64        `json:"averageRAM"`
	AverageVolumesUsagePercent  float64        `json:"averageVolumesUsagePercentage"`
	Requests                    float64        `json:"requests"`
	AnycastTraffic              float64        `json:"anycastTraffic"`
	Pods                        []OverviewPod  `json:"pods,omitempty"`
}

type OverviewPod struct {
	Name       string               `json:"name"`
	Status     string               `json:"status"`
	CPUUsage   float64              `json:"cpuUsage"`
	RAMUsage   float64              `json:"ramUsage"`
	Containers []OverviewContainer  `json:"containers,omitempty"`
}

type OverviewContainer struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	CPUUsage         float64 `json:"cpuUsage"`
	RAMUsage         float64 `json:"ramUsage"`
	Reason           string  `json:"reason,omitempty"`
	Message          string  `json:"message,omitempty"`
	Status           string  `json:"status"`
	Image            *string `json:"image,omitempty"`
	NumberOfRestarts *int32  `json:"numberOfRestarts,omitempty"`
	ExitCode         *int32  `json:"exitCode,omitempty"`
}

type DoubleStatusIndicator struct {
	Indicator   float64 `json:"indicator,omitempty"`
	StatusGrade string  `json:"statusGrade,omitempty"`
}

type Int32StatusIndicator struct {
	Indicator   int32  `json:"indicator,omitempty"`
	StatusGrade string `json:"statusGrade,omitempty"`
}

type Statistics struct {
	LatencyChart      map[string]float64 `json:"latencyChart,omitempty"`
	InstancesChart    map[string]*int64  `json:"instancesChart,omitempty"`
	CPUUsageChart     map[string]float64 `json:"cpuUsageChart,omitempty"`
	RAMUsageChart     map[string]float64 `json:"ramUsageChart,omitempty"`
	TrafficChart      map[string]float64 `json:"trafficChart,omitempty"`
	VolumesUsageChart map[string]float64 `json:"volumesUsageChart,omitempty"`
}

type UsageSummary struct {
	AverageLatency      float64           `json:"averageLatency,omitempty"`
	CurrentLatency      float64           `json:"currentLatency,omitempty"`
	TotalVolumeSizeInGB float64           `json:"totalVolumeSizeInGb,omitempty"`
	MonthlyCost         float64           `json:"monthlyCost,omitempty"`
	LatencyChart        map[string]float64 `json:"latencyChart,omitempty"`
	Status              ApplicationStatus `json:"status,omitempty"`
}

// --- User Limits ---

type UserLimits struct {
	MaxApplications              int32  `json:"maxNumberOfApplications,omitempty"`
	ExistingApplications         int32  `json:"existingNumberOfApplications,omitempty"`
	MaxRegionsPerApp             *int32 `json:"maxNumberOfRegionsPerApplication,omitempty"`
	MaxInstancesPerRegion        int32  `json:"maxNumberOfInstancesPerRegion,omitempty"`
	MaxInstancesPerApp           *int32 `json:"maxNumberOfInstancesPerApplication,omitempty"`
	MaxVolumesPerApp             int32  `json:"maxNumberOfVolumesPerApplication,omitempty"`
	MaxVolumeSize                *int32 `json:"maxVolumeSize,omitempty"`
}

// Log Forwarding

type LogForwardingConfig struct {
	ID        string `json:"id,omitempty"`
	App       string `json:"app"`
	Type      string `json:"type"`
	Endpoint  string `json:"endpoint"`
	Port      int32  `json:"port"`
	Format    string `json:"format"`
	Enabled   bool   `json:"enabled"`
	Token     string `json:"token,omitempty"`
	CreatedAt string `json:"createdAt,omitempty"`
}

type ListLogForwardingResponse struct {
	Items []LogForwardingConfig `json:"items,omitempty"`
}

// ============================================================================
// DNS Types (from Core API)
// ============================================================================

type DNSZone struct {
	ID        int64    `json:"Id"`
	Domain    string   `json:"Domain"`
	Status    *bool    `json:"Status,omitempty"`
	DateCreated *string `json:"DateCreated,omitempty"`
	DateModified *string `json:"DateModified,omitempty"`
	Nameservers []string `json:"Nameservers,omitempty"`
	HasDNSSec bool     `json:"HasDnsSec,omitempty"`
}

type DNSZoneAddModel struct {
	Domain string `json:"domain"`
}

type UpdateDNSZoneModel struct {
	LoggingEnabled       *bool   `json:"loggingEnabled,omitempty"`
	LoggingIPAnonymization *bool `json:"loggingIpAnonymizationEnabled,omitempty"`
}

type DNSRecord struct {
	ID                      int64                       `json:"Id"`
	Type                    int32                       `json:"Type"`
	Name                    string                      `json:"Name"`
	Value                   string                      `json:"Value"`
	TTL                     int32                       `json:"Ttl"`
	Priority                *int32                      `json:"Priority,omitempty"`
	Weight                  *int32                      `json:"Weight,omitempty"`
	Port                    *int32                      `json:"Port,omitempty"`
	Flags                   *int32                      `json:"Flags,omitempty"`
	Tag                     *string                     `json:"Tag,omitempty"`
	Disabled                bool                        `json:"Disabled,omitempty"`
	Accelerated             bool                        `json:"Accelerated,omitempty"`
	AcceleratedPullZoneID   int64                       `json:"AcceleratedPullZoneId,omitempty"`
	LinkName                *string                     `json:"LinkName,omitempty"`
	IPGeoLocationInfo       *GeoDNSLocationModel        `json:"IPGeoLocationInfo,omitempty"`
	GeolocationInfo         *DNSRecordGeoLocationInfo   `json:"GeolocationInfo,omitempty"`
	MonitorStatus           int32                       `json:"MonitorStatus,omitempty"`
	MonitorType             int32                       `json:"MonitorType,omitempty"`
	GeolocationLatitude     *float64                    `json:"GeolocationLatitude,omitempty"`
	GeolocationLongitude    *float64                    `json:"GeolocationLongitude,omitempty"`
	EnviromentalVariables   []DNSRecordEnvVariable      `json:"EnviromentalVariables,omitempty"`
	LatencyZone             *string                     `json:"LatencyZone,omitempty"`
	SmartRoutingType        int32                       `json:"SmartRoutingType,omitempty"`
	Comment                 *string                     `json:"Comment,omitempty"`
	AutoSSLIssuance         *bool                       `json:"AutoSslIssuance,omitempty"`
	AccelerationStatus      int32                       `json:"AccelerationStatus,omitempty"`
}

type AddDNSRecordModel struct {
	Type                  int32                   `json:"type"`
	Name                  string                  `json:"name"`
	Value                 string                  `json:"value"`
	TTL                   int32                   `json:"ttl"`
	Priority              *int32                  `json:"priority,omitempty"`
	Weight                *int32                  `json:"weight,omitempty"`
	Port                  *int32                  `json:"port,omitempty"`
	Flags                 *int32                  `json:"flags,omitempty"`
	Tag                   *string                 `json:"tag,omitempty"`
	PullZoneID            *int64                  `json:"pullZoneId,omitempty"`
	ScriptID              *int64                  `json:"scriptId,omitempty"`
	Accelerated           *bool                   `json:"accelerated,omitempty"`
	MonitorType           *DNSMonitoringType      `json:"monitorType,omitempty"`
	GeolocationLatitude   *float64                `json:"geolocationLatitude,omitempty"`
	GeolocationLongitude  *float64                `json:"geolocationLongitude,omitempty"`
	LatencyZone           *string                 `json:"latencyZone,omitempty"`
	SmartRoutingType      *DNSSmartRoutingType    `json:"smartRoutingType,omitempty"`
	EnviromentalVariables []DNSRecordEnvVariable  `json:"enviromentalVariables,omitempty"`
	Comment               *string                 `json:"comment,omitempty"`
	AutoSSLIssuance       *bool                   `json:"autoSslIssuance,omitempty"`
}

type UpdateDNSRecordModel struct {
	Type                  int32                   `json:"type"`
	Name                  string                  `json:"name"`
	Value                 string                  `json:"value"`
	TTL                   int32                   `json:"ttl"`
	Priority              *int32                  `json:"priority,omitempty"`
	Weight                *int32                  `json:"weight,omitempty"`
	Port                  *int32                  `json:"port,omitempty"`
	Flags                 *int32                  `json:"flags,omitempty"`
	Tag                   *string                 `json:"tag,omitempty"`
	Disabled              bool                    `json:"disabled,omitempty"`
	PullZoneID            *int64                  `json:"pullZoneId,omitempty"`
	ScriptID              *int64                  `json:"scriptId,omitempty"`
	Accelerated           *bool                   `json:"accelerated,omitempty"`
	MonitorType           *DNSMonitoringType      `json:"monitorType,omitempty"`
	GeolocationLatitude   *float64                `json:"geolocationLatitude,omitempty"`
	GeolocationLongitude  *float64                `json:"geolocationLongitude,omitempty"`
	LatencyZone           *string                 `json:"latencyZone,omitempty"`
	SmartRoutingType      *DNSSmartRoutingType    `json:"smartRoutingType,omitempty"`
	EnviromentalVariables []DNSRecordEnvVariable  `json:"enviromentalVariables,omitempty"`
	Comment               *string                 `json:"comment,omitempty"`
	AutoSSLIssuance       *bool                   `json:"autoSslIssuance,omitempty"`
}

// DNS Record Types (Bunny API spec values)
const (
	DNSRecordA        int32 = 0
	DNSRecordAAAA     int32 = 1
	DNSRecordCNAME    int32 = 2
	DNSRecordTXT      int32 = 3
	DNSRecordMX       int32 = 4
	DNSRecordRedirect int32 = 5
	DNSRecordFlatten  int32 = 6
	DNSRecordPullZone int32 = 7
	DNSRecordSRV      int32 = 8
	DNSRecordCAA      int32 = 9
	DNSRecordPTR      int32 = 10
	DNSRecordScript   int32 = 11
	DNSRecordNS       int32 = 12
	DNSRecordSVCB     int32 = 13
	DNSRecordHTTPS    int32 = 14
	DNSRecordTLSA     int32 = 15
)

// DNS Geo-location & Smart Routing Types

type GeoDNSLocationModel struct {
	CountryCode      string  `json:"CountryCode,omitempty"`
	Country          string  `json:"Country,omitempty"`
	ASN              int64   `json:"ASN,omitempty"`
	OrganizationName string  `json:"OrganizationName,omitempty"`
	City             *string `json:"City,omitempty"`
}

type DNSRecordGeoLocationInfo struct {
	Country   string   `json:"Country,omitempty"`
	City      string   `json:"City,omitempty"`
	Latitude  *float64 `json:"Latitude,omitempty"`
	Longitude *float64 `json:"Longitude,omitempty"`
}

type DNSMonitoringStatus int32

const (
	DNSMonitorUnknown DNSMonitoringStatus = 0
	DNSMonitorOnline  DNSMonitoringStatus = 1
	DNSMonitorOffline DNSMonitoringStatus = 2
)

type DNSMonitoringType int32

const (
	DNSMonitorNone    DNSMonitoringType = 0
	DNSMonitorPing    DNSMonitoringType = 1
	DNSMonitorHTTP    DNSMonitoringType = 2
	DNSMonitorMonitor DNSMonitoringType = 3
)

type DNSRecordEnvVariable struct {
	Name  string `json:"Name,omitempty"`
	Value string `json:"Value,omitempty"`
}

type DNSSmartRoutingType int32

const (
	DNSRouteNone        DNSSmartRoutingType = 0
	DNSRouteLatency     DNSSmartRoutingType = 1
	DNSRouteGeolocation DNSSmartRoutingType = 2
)

type AcceleratedStatus int32

const (
	AccelNone      AcceleratedStatus = 0
	AccelPending   AcceleratedStatus = 1
	AccelProcessing AcceleratedStatus = 2
	AccelCompleted AcceleratedStatus = 3
	AccelFailed    AcceleratedStatus = 4
)

// DNS Zone Import

type DNSZoneImportResult struct {
	Imported int32 `json:"imported,omitempty"`
	Failed   int32 `json:"failed,omitempty"`
	Total    int32 `json:"total,omitempty"`
}

// ============================================================================
// CDN / Pull Zone Types (from Core API)
// ============================================================================

type PullZone struct {
	ID              int64       `json:"Id"`
	Name            string      `json:"Name"`
	OriginURL       string      `json:"OriginUrl,omitempty"`
	Type            int32       `json:"Type,omitempty"`
	StorageZoneID   int64       `json:"StorageZoneId,omitempty"`
	Enabled         bool        `json:"Enabled"`
	Hostnames       []Hostname  `json:"Hostnames,omitempty"`
	ConnectionsLimit *int64     `json:"ConnectionsLimit,omitempty"`
	MonthlyBandwidthLimit *int64 `json:"MonthlyBandwidthLimit,omitempty"`
}

type PullZoneAddModel struct {
	Name          string `json:"name"`
	OriginURL     string `json:"originUrl,omitempty"`
	Type          *int32 `json:"type,omitempty"`
	StorageZoneID *int64 `json:"storageZoneId,omitempty"`
}

type PullZoneSettingsModel struct {
	Enabled                 *bool   `json:"enabled,omitempty"`
	CacheExpirationTime     *int32  `json:"cacheExpirationTime,omitempty"`
	BlockedIPs              []string `json:"blockedIps,omitempty"`
	BlockedReferrers        []string `json:"blockedReferrers,omitempty"`
	AllowedReferrers        []string `json:"allowedReferrers,omitempty"`
}

type Hostname struct {
	ID         int64  `json:"id,omitempty"`
	Value      string `json:"value,omitempty"`
	ForceSSL   bool   `json:"forceSsl,omitempty"`
	IsSystem   bool   `json:"isSystem,omitempty"`
}

type EdgeRule struct {
	ID          string         `json:"id,omitempty"`
	Description string         `json:"description,omitempty"`
	Enabled     bool           `json:"enabled"`
	Triggers    []EdgeRuleTrigger `json:"triggers,omitempty"`
	Actions     []EdgeRuleAction  `json:"actions,omitempty"`
}

type EdgeRuleTrigger struct {
	Type           int32    `json:"type"`
	Parameter1     string   `json:"parameter1,omitempty"`
	Parameter2     string   `json:"parameter2,omitempty"`
}

type EdgeRuleAction struct {
	Type       int32  `json:"type"`
	Parameter1 string `json:"parameter1,omitempty"`
	Parameter2 string `json:"parameter2,omitempty"`
}

type PullZonePurgeModel struct {
	URL string `json:"url,omitempty"`
}

type PullZoneCount struct {
	Count int64 `json:"count,omitempty"`
}

type AddHostnameRequestModel struct {
	Hostname string `json:"hostname"`
}

type RemoveHostnameRequestModel struct {
	Hostname string `json:"hostname"`
}
