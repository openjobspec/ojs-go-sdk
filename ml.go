package ojs

// MLResources declares compute resource requirements for ML/AI jobs.
// Use the builder methods to construct a resource specification, then
// pass it to WithMLResources when enqueuing a job.
type MLResources struct {
	GPUType             string  `json:"gpu_type,omitempty"`
	GPUCount            int     `json:"gpu_count,omitempty"`
	GPUMemoryGB         float64 `json:"gpu_memory_gb,omitempty"`
	MemoryGB            float64 `json:"memory_gb,omitempty"`
	CPUCores            int     `json:"cpu_cores,omitempty"`
	ModelID             string  `json:"model_id,omitempty"`
	ModelVersion        string  `json:"model_version,omitempty"`
	Runtime             string  `json:"runtime,omitempty"`
	AcceleratorRequired bool    `json:"accelerator_required,omitempty"`
}

// NewMLResources creates a new MLResources builder.
func NewMLResources() MLResources {
	return MLResources{}
}

// WithGPUType sets the required GPU type.
func (r MLResources) WithGPUType(gpuType string) MLResources {
	r.GPUType = gpuType
	return r
}

// WithGPUCount sets the number of GPUs required.
func (r MLResources) WithGPUCount(count int) MLResources {
	r.GPUCount = count
	return r
}

// WithGPUMemoryGB sets the minimum GPU memory per device in gigabytes.
func (r MLResources) WithGPUMemoryGB(gb float64) MLResources {
	r.GPUMemoryGB = gb
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

// WithModelID sets the model identifier.
func (r MLResources) WithModelID(id string) MLResources {
	r.ModelID = id
	return r
}

// WithModelVersion sets the model semantic version.
func (r MLResources) WithModelVersion(version string) MLResources {
	r.ModelVersion = version
	return r
}

// WithRuntime sets the ML runtime.
func (r MLResources) WithRuntime(runtime string) MLResources {
	r.Runtime = runtime
	return r
}

// WithAcceleratorRequired sets whether a hardware accelerator is required.
func (r MLResources) WithAcceleratorRequired(required bool) MLResources {
	r.AcceleratorRequired = required
	return r
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
