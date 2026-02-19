// Package ml provides ML/AI resource requirement types and helpers for OJS jobs.
//
// This extension follows the OJS ML Resource Extension Specification,
// enabling jobs to declare GPU, CPU, memory, and storage requirements
// as well as model references and checkpoint configuration.
//
// Resource requirements are stored in the job's meta field and require
// no changes to the core OJS specification. Backends that do not
// understand resource requirements ignore them.
package ml

import ojs "github.com/openjobspec/ojs-go-sdk"

// GPU type constants for common accelerator hardware.
const (
	GPUNvidiaA100  = "nvidia-a100"
	GPUNvidiaH100  = "nvidia-h100"
	GPUNvidiaT4    = "nvidia-t4"
	GPUNvidiaL4    = "nvidia-l4"
	GPUNvidiaV100  = "nvidia-v100"
	GPUAmdMI250    = "amd-mi250"
	GPUAmdMI300X   = "amd-mi300x"
	GPUGoogleTPUv5 = "google-tpu-v5"
)

// GPURequirements declares GPU resource needs.
type GPURequirements struct {
	Type     string  `json:"type,omitempty"`
	Count    int     `json:"count,omitempty"`
	MemoryGB float64 `json:"memory_gb,omitempty"`
}

// CPURequirements declares CPU resource needs.
type CPURequirements struct {
	Cores int `json:"cores,omitempty"`
}

// ResourceRequirements declares compute resource needs for a job,
// following the schema in meta.resources from the spec.
type ResourceRequirements struct {
	GPU       *GPURequirements `json:"gpu,omitempty"`
	CPU       *CPURequirements `json:"cpu,omitempty"`
	MemoryGB  float64          `json:"memory_gb,omitempty"`
	StorageGB float64          `json:"storage_gb,omitempty"`
}

// ModelReference identifies an ML model artifact.
type ModelReference struct {
	Name     string `json:"name"`
	Version  string `json:"version,omitempty"`
	Registry string `json:"registry,omitempty"`
	Checksum string `json:"checksum,omitempty"`
}

// CheckpointConfig configures periodic checkpointing for long-running jobs.
type CheckpointConfig struct {
	Enabled        bool   `json:"enabled"`
	IntervalSec    int    `json:"interval_s,omitempty"`
	StorageURI     string `json:"storage_uri,omitempty"`
	MaxCheckpoints int    `json:"max_checkpoints,omitempty"`
}

// PreemptionConfig declares a job's preemption tolerance.
type PreemptionConfig struct {
	Preemptible         bool `json:"preemptible"`
	GracePeriodSec      int  `json:"grace_period_s,omitempty"`
	CheckpointOnPreempt bool `json:"checkpoint_on_preempt,omitempty"`
}

// toResourceMap converts ResourceRequirements to a wire-format map.
func toResourceMap(req ResourceRequirements) map[string]any {
	res := make(map[string]any)
	if req.GPU != nil {
		gpu := map[string]any{"count": req.GPU.Count}
		if req.GPU.Type != "" {
			gpu["type"] = req.GPU.Type
		}
		if req.GPU.MemoryGB > 0 {
			gpu["memory_gb"] = req.GPU.MemoryGB
		}
		res["gpu"] = gpu
	}
	if req.CPU != nil {
		res["cpu"] = map[string]any{"cores": req.CPU.Cores}
	}
	if req.MemoryGB > 0 {
		res["memory_gb"] = req.MemoryGB
	}
	if req.StorageGB > 0 {
		res["storage_gb"] = req.StorageGB
	}
	return res
}

// toModelMap converts a ModelReference to a wire-format map.
func toModelMap(ref ModelReference) map[string]any {
	m := map[string]any{"name": ref.Name}
	if ref.Version != "" {
		m["version"] = ref.Version
	}
	if ref.Registry != "" {
		m["registry"] = ref.Registry
	}
	if ref.Checksum != "" {
		m["checksum"] = ref.Checksum
	}
	return m
}

// WithGPU returns an EnqueueOption that sets GPU resource requirements
// in the job's meta.resources field. This is a convenience shorthand
// for WithResources with only GPU fields populated.
func WithGPU(gpuType string, count int, memoryGB float64) ojs.EnqueueOption {
	return WithResources(ResourceRequirements{
		GPU: &GPURequirements{
			Type:     gpuType,
			Count:    count,
			MemoryGB: memoryGB,
		},
	})
}

// WithModel returns an EnqueueOption that sets a model reference
// in the job's meta.model field.
func WithModel(ref ModelReference) ojs.EnqueueOption {
	return ojs.WithMeta(map[string]any{
		"model": toModelMap(ref),
	})
}

// WithResources returns an EnqueueOption that sets resource
// requirements in the job's meta.resources field.
func WithResources(req ResourceRequirements) ojs.EnqueueOption {
	return ojs.WithMeta(map[string]any{
		"resources": toResourceMap(req),
	})
}

// WithCheckpoint returns an EnqueueOption that sets checkpoint
// configuration in the job's meta.checkpoint field.
func WithCheckpoint(cfg CheckpointConfig) ojs.EnqueueOption {
	ckpt := map[string]any{"enabled": cfg.Enabled}
	if cfg.IntervalSec > 0 {
		ckpt["interval_s"] = cfg.IntervalSec
	}
	if cfg.StorageURI != "" {
		ckpt["storage_uri"] = cfg.StorageURI
	}
	if cfg.MaxCheckpoints > 0 {
		ckpt["max_checkpoints"] = cfg.MaxCheckpoints
	}
	return ojs.WithMeta(map[string]any{
		"checkpoint": ckpt,
	})
}

// WithPreemption returns an EnqueueOption that sets preemption
// configuration in the job's meta.preemption field.
func WithPreemption(cfg PreemptionConfig) ojs.EnqueueOption {
	p := map[string]any{"preemptible": cfg.Preemptible}
	if cfg.GracePeriodSec > 0 {
		p["grace_period_s"] = cfg.GracePeriodSec
	}
	if cfg.CheckpointOnPreempt {
		p["checkpoint_on_preempt"] = cfg.CheckpointOnPreempt
	}
	return ojs.WithMeta(map[string]any{
		"preemption": p,
	})
}
