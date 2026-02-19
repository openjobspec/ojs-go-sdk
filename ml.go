package ojs

import "fmt"

// MLResources declares compute resource requirements for ML/AI jobs.
// Use the builder methods to construct a resource specification, then
// pass it to WithMLResources when enqueuing a job.
//
// See the OJS ML/AI Resource Extension Specification (spec/spec/ojs-ml-resources.md)
// for the full schema definition.
type MLResources struct {
	// Resource Requirements
	Accelerator          string  `json:"ext_ml_accelerator,omitempty"`
	GPUType              string  `json:"ext_ml_gpu_type,omitempty"`
	GPUCount             int     `json:"ext_ml_gpu_count,omitempty"`
	GPUMemoryGB          float64 `json:"ext_ml_gpu_memory_gb,omitempty"`
	GPUComputeCapability string  `json:"ext_ml_gpu_compute_capability,omitempty"`
	GPUInterconnect      string  `json:"ext_ml_gpu_interconnect,omitempty"`
	MemoryGB             float64 `json:"ext_ml_memory_gb,omitempty"`
	CPUCores             int     `json:"ext_ml_cpu_cores,omitempty"`
	StorageGB            float64 `json:"ext_ml_storage_gb,omitempty"`
	ShmSizeGB            float64 `json:"ext_ml_shm_size_gb,omitempty"`

	// TPU Requirements
	TPUType      string `json:"ext_ml_tpu_type,omitempty"`
	TPUTopology  string `json:"ext_ml_tpu_topology,omitempty"`
	TPUChipCount int    `json:"ext_ml_tpu_chip_count,omitempty"`

	// Model Versioning
	ModelID       string `json:"ext_ml_model_id,omitempty"`
	ModelVersion  string `json:"ext_ml_model_version,omitempty"`
	ModelProvider string `json:"ext_ml_model_provider,omitempty"`
	ModelChecksum string `json:"ext_ml_model_checksum,omitempty"`
	ModelFormat   string `json:"ext_ml_model_format,omitempty"`

	// Compute Constraints
	MaxTokens           int    `json:"ext_ml_max_tokens,omitempty"`
	MaxBatchSize        int    `json:"ext_ml_max_batch_size,omitempty"`
	TimeoutSeconds      int    `json:"ext_ml_timeout_seconds,omitempty"`
	PriorityClass       string `json:"ext_ml_priority_class,omitempty"`
	Runtime             string `json:"ext_ml_runtime,omitempty"`
	Precision           string `json:"ext_ml_precision,omitempty"`
	DistributedStrategy string `json:"ext_ml_distributed_strategy,omitempty"`
}

// NewMLResources creates a new MLResources builder.
func NewMLResources() MLResources {
	return MLResources{}
}

// --- Resource Requirements ---

// WithAccelerator sets the required accelerator type (e.g., "gpu", "tpu", "fpga", "cpu").
func (r MLResources) WithAccelerator(accelerator string) MLResources {
	r.Accelerator = accelerator
	return r
}

// WithGPUType sets the required GPU type (e.g., "nvidia-a100", "nvidia-h100").
func (r MLResources) WithGPUType(gpuType string) MLResources {
	r.GPUType = gpuType
	return r
}

// WithGPUCount sets the number of GPUs required.
func (r MLResources) WithGPUCount(count int) MLResources {
	r.GPUCount = count
	return r
}

// WithGPUMemoryGB sets the minimum GPU VRAM per device in gigabytes.
func (r MLResources) WithGPUMemoryGB(gb float64) MLResources {
	r.GPUMemoryGB = gb
	return r
}

// WithGPUComputeCapability sets the minimum NVIDIA compute capability (e.g., "8.0", "9.0").
func (r MLResources) WithGPUComputeCapability(cc string) MLResources {
	r.GPUComputeCapability = cc
	return r
}

// WithGPUInterconnect sets the required GPU interconnect: "nvlink", "pcie", or "any".
func (r MLResources) WithGPUInterconnect(interconnect string) MLResources {
	r.GPUInterconnect = interconnect
	return r
}

// WithMemoryGB sets the minimum system memory in gigabytes.
func (r MLResources) WithMemoryGB(gb float64) MLResources {
	r.MemoryGB = gb
	return r
}

// WithCPUCores sets the minimum CPU cores required.
func (r MLResources) WithCPUCores(cores int) MLResources {
	r.CPUCores = cores
	return r
}

// WithStorageGB sets the minimum scratch storage in gigabytes.
func (r MLResources) WithStorageGB(gb float64) MLResources {
	r.StorageGB = gb
	return r
}

// WithShmSizeGB sets the minimum shared memory (/dev/shm) size in gigabytes.
func (r MLResources) WithShmSizeGB(gb float64) MLResources {
	r.ShmSizeGB = gb
	return r
}

// --- TPU Requirements ---

// WithTPUType sets the TPU version: "v4", "v5e", "v5p", "v6e".
func (r MLResources) WithTPUType(tpuType string) MLResources {
	r.TPUType = tpuType
	return r
}

// WithTPUTopology sets the TPU pod slice topology (e.g., "2x4", "4x4").
func (r MLResources) WithTPUTopology(topology string) MLResources {
	r.TPUTopology = topology
	return r
}

// WithTPUChipCount sets the number of TPU chips required.
func (r MLResources) WithTPUChipCount(count int) MLResources {
	r.TPUChipCount = count
	return r
}

// --- Model Versioning ---

// WithModelID sets the model identifier (e.g., "llama-3.1-70b", "gpt-4").
func (r MLResources) WithModelID(id string) MLResources {
	r.ModelID = id
	return r
}

// WithModelVersion sets the model semantic version (e.g., "v1.0", "v2.1").
func (r MLResources) WithModelVersion(version string) MLResources {
	r.ModelVersion = version
	return r
}

// WithModelProvider sets the model provider (e.g., "openai", "anthropic", "huggingface").
func (r MLResources) WithModelProvider(provider string) MLResources {
	r.ModelProvider = provider
	return r
}

// WithModelChecksum sets the integrity checksum (e.g., "sha256:abc123def...").
func (r MLResources) WithModelChecksum(checksum string) MLResources {
	r.ModelChecksum = checksum
	return r
}

// WithModelFormat sets the model format: "safetensors", "gguf", "onnx", "torchscript", "savedmodel", "custom".
func (r MLResources) WithModelFormat(format string) MLResources {
	r.ModelFormat = format
	return r
}

// --- Compute Constraints ---

// WithMaxTokens sets the maximum tokens for generation tasks.
func (r MLResources) WithMaxTokens(tokens int) MLResources {
	r.MaxTokens = tokens
	return r
}

// WithMaxBatchSize sets the maximum batch size for inference.
func (r MLResources) WithMaxBatchSize(size int) MLResources {
	r.MaxBatchSize = size
	return r
}

// WithTimeoutSeconds sets the ML-specific timeout in seconds.
func (r MLResources) WithTimeoutSeconds(seconds int) MLResources {
	r.TimeoutSeconds = seconds
	return r
}

// WithPriorityClass sets the resource priority class (e.g., "spot", "on-demand", "reserved").
func (r MLResources) WithPriorityClass(class string) MLResources {
	r.PriorityClass = class
	return r
}

// WithRuntime sets the ML runtime: "pytorch", "tensorflow", "onnx", "triton", "vllm", "tgi", "custom".
func (r MLResources) WithRuntime(runtime string) MLResources {
	r.Runtime = runtime
	return r
}

// WithPrecision sets the compute precision: "fp32", "fp16", "bf16", "fp8", "int8", "int4".
func (r MLResources) WithPrecision(precision string) MLResources {
	r.Precision = precision
	return r
}

// WithDistributedStrategy sets the distribution strategy: "none", "data_parallel",
// "tensor_parallel", "pipeline_parallel", "fsdp", "deepspeed".
func (r MLResources) WithDistributedStrategy(strategy string) MLResources {
	r.DistributedStrategy = strategy
	return r
}

// Validate checks that resource requirements are logically consistent.
func (r MLResources) Validate() error {
	if r.GPUCount < 0 {
		return fmt.Errorf("ojs: gpu_count must be non-negative, got %d", r.GPUCount)
	}
	if r.GPUMemoryGB < 0 {
		return fmt.Errorf("ojs: gpu_memory_gb must be non-negative, got %f", r.GPUMemoryGB)
	}
	if r.MemoryGB < 0 {
		return fmt.Errorf("ojs: memory_gb must be non-negative, got %f", r.MemoryGB)
	}
	if r.CPUCores < 0 {
		return fmt.Errorf("ojs: cpu_cores must be non-negative, got %d", r.CPUCores)
	}
	if r.StorageGB < 0 {
		return fmt.Errorf("ojs: storage_gb must be non-negative, got %f", r.StorageGB)
	}
	if r.ShmSizeGB < 0 {
		return fmt.Errorf("ojs: shm_size_gb must be non-negative, got %f", r.ShmSizeGB)
	}
	if r.GPUMemoryGB > 0 && r.GPUCount == 0 {
		return fmt.Errorf("ojs: gpu_memory_gb requires gpu_count > 0")
	}
	if r.GPUType != "" && r.GPUCount == 0 {
		return fmt.Errorf("ojs: gpu_type requires gpu_count > 0")
	}
	if r.GPUComputeCapability != "" && r.GPUCount == 0 {
		return fmt.Errorf("ojs: gpu_compute_capability requires gpu_count > 0")
	}
	if r.GPUInterconnect != "" && r.GPUCount < 2 {
		return fmt.Errorf("ojs: gpu_interconnect requires gpu_count >= 2")
	}
	if r.TPUChipCount < 0 {
		return fmt.Errorf("ojs: tpu_chip_count must be non-negative, got %d", r.TPUChipCount)
	}
	if r.TPUTopology != "" && r.TPUType == "" {
		return fmt.Errorf("ojs: tpu_topology requires tpu_type to be set")
	}
	if r.MaxTokens < 0 {
		return fmt.Errorf("ojs: max_tokens must be non-negative, got %d", r.MaxTokens)
	}
	if r.MaxBatchSize < 0 {
		return fmt.Errorf("ojs: max_batch_size must be non-negative, got %d", r.MaxBatchSize)
	}
	if r.TimeoutSeconds < 0 {
		return fmt.Errorf("ojs: timeout_seconds must be non-negative, got %d", r.TimeoutSeconds)
	}

	// Validate accelerator consistency
	if r.Accelerator == "tpu" && r.GPUCount > 0 {
		return fmt.Errorf("ojs: accelerator=tpu is incompatible with gpu_count > 0")
	}
	if r.Accelerator == "cpu" && (r.GPUCount > 0 || r.TPUChipCount > 0) {
		return fmt.Errorf("ojs: accelerator=cpu is incompatible with gpu_count or tpu_chip_count > 0")
	}

	return nil
}

// WithMLResources attaches ML resource requirements to a job as the
// ext_ml_resources extension in the job's meta field.
func WithMLResources(res MLResources) EnqueueOption {
	return func(c *enqueueConfig) {
		if c.meta == nil {
			c.meta = make(map[string]any)
		}
		c.meta["ext_ml_resources"] = res
	}
}

// --- GPU Affinity ---

// AffinityOperator defines comparison logic for affinity rules.
type AffinityOperator string

const (
	AffinityIn           AffinityOperator = "In"
	AffinityNotIn        AffinityOperator = "NotIn"
	AffinityExists       AffinityOperator = "Exists"
	AffinityDoesNotExist AffinityOperator = "DoesNotExist"
	AffinityGt           AffinityOperator = "Gt"
	AffinityGte          AffinityOperator = "Gte"
	AffinityLt           AffinityOperator = "Lt"
	AffinityLte          AffinityOperator = "Lte"
)

// AffinityRule defines a scheduling constraint for worker selection.
type AffinityRule struct {
	Key      string           `json:"key"`
	Operator AffinityOperator `json:"operator"`
	Values   []string         `json:"values,omitempty"`
}

// WeightedAffinityRule is an affinity rule with a preference weight.
type WeightedAffinityRule struct {
	Key      string           `json:"key"`
	Operator AffinityOperator `json:"operator"`
	Values   []string         `json:"values,omitempty"`
	Weight   int              `json:"weight,omitempty"`
}

// Affinity declares scheduling preferences for job placement.
// Required rules are hard constraints; preferred rules are soft hints with weights.
type Affinity struct {
	Required  []AffinityRule         `json:"required,omitempty"`
	Preferred []WeightedAffinityRule `json:"preferred,omitempty"`
}

// WithAffinity attaches scheduling affinity rules to a job.
func WithAffinity(aff Affinity) EnqueueOption {
	return func(c *enqueueConfig) {
		if c.meta == nil {
			c.meta = make(map[string]any)
		}
		c.meta["ext_affinity"] = aff
	}
}

// WithNodeSelector attaches node selector labels to a job.
// All labels must match for a worker to be eligible (AND semantics).
func WithNodeSelector(labels map[string]string) EnqueueOption {
	return func(c *enqueueConfig) {
		if c.meta == nil {
			c.meta = make(map[string]any)
		}
		c.meta["ext_node_selector"] = labels
	}
}

// --- Checkpointing ---

// CheckpointConfig declares checkpoint behavior for long-running jobs.
type CheckpointConfig struct {
	Enabled        bool   `json:"enabled"`
	IntervalSec    int    `json:"interval_s,omitempty"`
	StorageURI     string `json:"storage_uri,omitempty"`
	MaxCheckpoints int    `json:"max_checkpoints,omitempty"`
}

// Checkpoint represents a saved snapshot of job progress.
type Checkpoint struct {
	JobID      string         `json:"job_id"`
	Epoch      int            `json:"epoch,omitempty"`
	Step       int            `json:"step,omitempty"`
	Loss       float64        `json:"loss,omitempty"`
	StorageKey string         `json:"storage_key,omitempty"`
	CreatedAt  string         `json:"created_at,omitempty"`
	Data       map[string]any `json:"data,omitempty"`
}

// WithCheckpointing enables checkpoint/resume for long-running ML jobs.
func WithCheckpointing(cfg CheckpointConfig) EnqueueOption {
	return func(c *enqueueConfig) {
		if c.meta == nil {
			c.meta = make(map[string]any)
		}
		c.meta["ext_checkpoint"] = cfg
	}
}

// GetLastCheckpoint extracts the last checkpoint from a job's metadata, if present.
// Workers should call this when starting a job to check if there is saved state
// to resume from.
func GetLastCheckpoint(job Job) (*Checkpoint, bool) {
	if job.Meta == nil {
		return nil, false
	}
	raw, ok := job.Meta["last_checkpoint"]
	if !ok {
		return nil, false
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil, false
	}
	cp := &Checkpoint{}
	if v, ok := m["job_id"].(string); ok {
		cp.JobID = v
	}
	if v, ok := m["epoch"].(float64); ok {
		cp.Epoch = int(v)
	}
	if v, ok := m["step"].(float64); ok {
		cp.Step = int(v)
	}
	if v, ok := m["loss"].(float64); ok {
		cp.Loss = v
	}
	if v, ok := m["storage_key"].(string); ok {
		cp.StorageKey = v
	}
	if v, ok := m["created_at"].(string); ok {
		cp.CreatedAt = v
	}
	if v, ok := m["data"].(map[string]any); ok {
		cp.Data = v
	}
	return cp, true
}

// --- Preemption ---

// PreemptionPolicy declares a job's tolerance for being preempted.
type PreemptionPolicy struct {
	Preemptible         bool `json:"preemptible"`
	GracePeriodSec      int  `json:"grace_period_s,omitempty"`
	CheckpointOnPreempt bool `json:"checkpoint_on_preempt,omitempty"`
}

// WithPreemptionPolicy attaches preemption configuration to a job.
func WithPreemptionPolicy(p PreemptionPolicy) EnqueueOption {
	return func(c *enqueueConfig) {
		if c.meta == nil {
			c.meta = make(map[string]any)
		}
		c.meta["ext_preemption"] = p
	}
}

// --- Resource Reservation ---

// ResourceReservation references a pre-allocated capacity guarantee.
type ResourceReservation struct {
	ReservationID  string `json:"reservation_id"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

// WithResourceReservation attaches a resource reservation to a job.
func WithResourceReservation(res ResourceReservation) EnqueueOption {
	return func(c *enqueueConfig) {
		if c.meta == nil {
			c.meta = make(map[string]any)
		}
		c.meta["ext_reservation"] = res
	}
}

// --- Worker Capabilities ---

// WorkerCapabilities describes the compute resources a worker can offer.
// Workers include this in FETCH requests to enable resource-aware scheduling.
type WorkerCapabilities struct {
	Accelerator string            `json:"accelerator,omitempty"`
	GPU         *GPUCapability    `json:"gpu,omitempty"`
	TPU         *TPUCapability    `json:"tpu,omitempty"`
	CPUCores    int               `json:"cpu_cores,omitempty"`
	MemoryGB    float64           `json:"memory_gb,omitempty"`
	StorageGB   float64           `json:"storage_gb,omitempty"`
	ShmSizeGB   float64           `json:"shm_size_gb,omitempty"`
	Models      []LoadedModel     `json:"models_loaded,omitempty"`
	Runtimes    []string          `json:"runtimes,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// GPUCapability describes a worker's GPU resources.
type GPUCapability struct {
	Count             int     `json:"count"`
	Type              string  `json:"type,omitempty"`
	MemoryGB          float64 `json:"memory_gb,omitempty"`
	ComputeCapability string  `json:"compute_capability,omitempty"`
	Interconnect      string  `json:"interconnect,omitempty"`
}

// TPUCapability describes a worker's TPU resources.
type TPUCapability struct {
	Type      string `json:"type,omitempty"`
	Topology  string `json:"topology,omitempty"`
	ChipCount int    `json:"chip_count,omitempty"`
}

// LoadedModel describes a model currently loaded on a worker.
type LoadedModel struct {
	ModelID      string `json:"model_id"`
	ModelVersion string `json:"model_version,omitempty"`
	ModelFormat  string `json:"model_format,omitempty"`
}

// WithWorkerCapabilities configures the worker to advertise its compute
// capabilities during FETCH and heartbeat requests.
func WithWorkerCapabilities(cap WorkerCapabilities) WorkerOption {
	return func(c *workerConfig) {
		c.capabilities = &cap
	}
}
