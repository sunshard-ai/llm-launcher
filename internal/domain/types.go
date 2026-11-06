// Package domain contains the versioned data shared by the detector, planner and runner.
package domain

type GPU struct {
	Index             string `json:"index"`
	UUID              string `json:"uuid,omitempty"`
	Vendor            string `json:"vendor"`
	Name              string `json:"name"`
	VRAMTotalBytes    uint64 `json:"vram_total_bytes"`
	VRAMFreeBytes     uint64 `json:"vram_free_bytes"`
	ComputeCapability string `json:"compute_capability,omitempty"`
	DriverVersion     string `json:"driver_version,omitempty"`
}

type DockerInfo struct {
	Installed    bool   `json:"installed"`
	Reachable    bool   `json:"reachable"`
	Compose      bool   `json:"compose"`
	ServerOS     string `json:"server_os,omitempty"`
	Architecture string `json:"architecture,omitempty"`
	Endpoint     string `json:"endpoint,omitempty"`
	Context      string `json:"context,omitempty"`
	MemoryBytes  uint64 `json:"memory_bytes"`
	CPUs         int    `json:"cpus"`
	Error        string `json:"error,omitempty"`
}

type Hardware struct {
	OS                string     `json:"os"`
	Distro            string     `json:"distro"`
	Arch              string     `json:"arch"`
	WSL               bool       `json:"wsl"`
	CPUName           string     `json:"cpu_name"`
	LogicalCPUs       int        `json:"logical_cpus"`
	RAMTotalBytes     uint64     `json:"ram_total_bytes"`
	RAMAvailableBytes uint64     `json:"ram_available_bytes"`
	DiskFreeBytes     uint64     `json:"disk_free_bytes"`
	DiskPath          string     `json:"disk_path"`
	GPUs              []GPU      `json:"gpus"`
	Docker            DockerInfo `json:"docker"`
	Warnings          []string   `json:"warnings,omitempty"`
}

type Model struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Tasks        []string `json:"tasks"`
	Runtime      string   `json:"runtime"`
	Ref          string   `json:"ref"`
	Revision     string   `json:"revision,omitempty"`
	ParametersB  float64  `json:"parameters_b"`
	Quantization string   `json:"quantization"`
	MinRAMGiB    float64  `json:"min_ram_gib"`
	MinVRAMGiB   float64  `json:"min_vram_gib"`
	DiskGiB      float64  `json:"disk_gib"`
	Context      int      `json:"context"`
	License      string   `json:"license"`
	URL          string   `json:"url"`
}

type Catalog struct {
	SchemaVersion int                       `json:"schema_version"`
	Updated       string                    `json:"updated"`
	Runtimes      map[string]string         `json:"runtimes"`
	Models        []Model                   `json:"models"`
	Profiles      map[string]CatalogProfile `json:"profiles,omitempty"`
}

type CatalogProfile struct {
	Purpose       string    `json:"purpose"`
	Format        string    `json:"format"`
	DownloadBytes int64     `json:"download_bytes,omitempty"`
	Source        string    `json:"source"`
	Verification  string    `json:"verification"`
	Artifact      *HFSource `json:"artifact,omitempty"`
}

type Recommendation struct {
	Model        Model    `json:"model"`
	Acceleration string   `json:"acceleration"`
	GPU          *GPU     `json:"gpu,omitempty"`
	Score        int      `json:"score"`
	Notes        []string `json:"notes"`
}

type Plan struct {
	SchemaVersion  int               `json:"schema_version"`
	Project        string            `json:"project"`
	CreatedAt      string            `json:"created_at"`
	Runtime        string            `json:"runtime"`
	Image          string            `json:"image"`
	Task           string            `json:"task"`
	Acceleration   string            `json:"acceleration"`
	DeviceIDs      []string          `json:"device_ids,omitempty"`
	Model          Model             `json:"model"`
	Context        int               `json:"context"`
	Parallel       int               `json:"parallel"`
	Port           int               `json:"port"`
	Host           string            `json:"host"`
	GPUUtilization float64           `json:"gpu_utilization"`
	Hardware       Hardware          `json:"hardware"`
	Advanced       *AdvancedSettings `json:"advanced,omitempty"`
	HFSource       *HFSource         `json:"hf_source,omitempty"`
}

type Status struct {
	Project     string `json:"project"`
	Runtime     string `json:"runtime"`
	Endpoint    string `json:"endpoint"`
	Model       string `json:"model"`
	Reachable   bool   `json:"reachable"`
	ModelLoaded bool   `json:"model_loaded"`
	Containers  string `json:"containers"`
	Error       string `json:"error,omitempty"`
}

type SmokeResult struct {
	OK         bool   `json:"ok"`
	Kind       string `json:"kind"`
	Output     string `json:"output"`
	DurationMS int64  `json:"duration_ms"`
}

// AdvancedSettings contains optional defaults and non-secret engine settings.
type AdvancedSettings struct {
	GPUMemoryUtilization *float64          `json:"gpu_memory_utilization,omitempty"`
	PresencePenalty      *float64          `json:"presence_penalty,omitempty"`
	FrequencyPenalty     *float64          `json:"frequency_penalty,omitempty"`
	DType                string            `json:"dtype,omitempty"`
	EnforceEager         bool              `json:"enforce_eager,omitempty"`
	Temperature          *float64          `json:"temperature,omitempty"`
	MaxTokens            *int              `json:"max_tokens,omitempty"`
	TopP                 *float64          `json:"top_p,omitempty"`
	TopK                 *int              `json:"top_k,omitempty"`
	MinP                 *float64          `json:"min_p,omitempty"`
	RepeatPenalty        *float64          `json:"repeat_penalty,omitempty"`
	RepeatLastN          *int              `json:"repeat_last_n,omitempty"`
	Seed                 *int              `json:"seed,omitempty"`
	Stop                 []string          `json:"stop,omitempty"`
	Environment          map[string]string `json:"environment,omitempty"`
}

type HFSource struct {
	Repo     string       `json:"repo"`
	Revision string       `json:"revision"`
	File     string       `json:"file,omitempty"`
	Bytes    int64        `json:"bytes"`
	SHA256   string       `json:"sha256,omitempty"`
	Files    []HFArtifact `json:"files,omitempty"`
}

type HFArtifact struct {
	Name   string `json:"name"`
	Bytes  int64  `json:"bytes"`
	SHA256 string `json:"sha256,omitempty"`
	BlobID string `json:"blob_id,omitempty"`
}
