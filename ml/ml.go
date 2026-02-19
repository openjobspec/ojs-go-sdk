// Package ml provides ML/AI resource requirement types and helpers for OJS jobs.
//
// This extension follows the OJS ML Resource Extension Specification,
// enabling jobs to declare GPU, TPU, CPU, memory, and storage requirements
// as well as model references, checkpoint configuration, and preemption policies.
//
// Resource requirements are stored in the job's meta field and require
// no changes to the core OJS specification. Backends that do not
// understand resource requirements ignore them.
package ml

import (
	"fmt"

	ojs "github.com/openjobspec/ojs-go-sdk"
)

// GPU type constants for common accelerator hardware.
const (
	GPUNvidiaA100  = "nvidia-a100"
	GPUNvidiaH100  = "nvidia-h100"
	GPUNvidiaH200  = "nvidia-h200"
	GPUNvidiaT4    = "nvidia-t4"
	GPUNvidiaL4    = "nvidia-l4"
	GPUNvidiaL40S  = "nvidia-l40s"
	GPUNvidiaV100  = "nvidia-v100"
	GPUNvidiaA10G  = "nvidia-a10g"
	GPUNvidiaB200  = "nvidia-b200"
	GPUAmdMI250    = "amd-mi250"
	GPUAmdMI300X   = "amd-mi300x"
	GPUGoogleTPUv5 = "google-tpu-v5"
)

// TPU type constants.
const (
	TPUv4  = "v4"
	TPUv5e = "v5e"
	TPUv5p = "v5p"
	TPUv6e = "v6e"
)

// Precision constants for compute precision.
const (
	PrecisionFP32 = "fp32"
	PrecisionFP16 = "fp16"
	PrecisionBF16 = "bf16"
	PrecisionFP8  = "fp8"
	PrecisionINT8 = "int8"
	PrecisionINT4 = "int4"
)

// Runtime constants for ML frameworks.
const (
	RuntimePyTorch    = "pytorch"
	RuntimeTensorFlow = "tensorflow"
	RuntimeONNX       = "onnx"
	RuntimeTriton     = "triton"
	RuntimeVLLM       = "vllm"
	RuntimeTGI        = "tgi"
	RuntimeCustom     = "custom"
)

// DistributedStrategy constants.
const (
	StrategyNone             = "none"
	StrategyDataParallel     = "data_parallel"
	StrategyTensorParallel   = "tensor_parallel"
	StrategyPipelineParallel = "pipeline_parallel"
	StrategyFSDP             = "fsdp"
	StrategyDeepSpeed        = "deepspeed"
)

// Interconnect constants.
const (
	InterconnectNVLink = "nvlink"
	InterconnectPCIe   = "pcie"
	InterconnectAny    = "any"
)

// GPURequirements declares GPU resource needs.
type GPURequirements struct {
	Type              string  `json:"type,omitempty"`
	Count             int     `json:"count,omitempty"`
	MemoryGB          float64 `json:"memory_gb,omitempty"`
	ComputeCapability string  `json:"compute_capability,omitempty"`
	Interconnect      string  `json:"interconnect,omitempty"`
}

// TPURequirements declares TPU resource needs.
type TPURequirements struct {
	Type      string `json:"type,omitempty"`
	Topology  string `json:"topology,omitempty"`
	ChipCount int    `json:"chip_count,omitempty"`
}

// CPURequirements declares CPU resource needs.
type CPURequirements struct {
	Cores int `json:"cores,omitempty"`
}

// ResourceRequirements declares compute resource needs for a job,
// following the schema in meta.resources from the spec.
type ResourceRequirements struct {
	GPU       *GPURequirements `json:"gpu,omitempty"`
	TPU       *TPURequirements `json:"tpu,omitempty"`
	CPU       *CPURequirements `json:"cpu,omitempty"`
	MemoryGB  float64          `json:"memory_gb,omitempty"`
	StorageGB float64          `json:"storage_gb,omitempty"`
	ShmSizeGB float64          `json:"shm_size_gb,omitempty"`
}

// ModelReference identifies an ML model artifact.
type ModelReference struct {
	Name     string `json:"name"`
	Version  string `json:"version,omitempty"`
	Registry string `json:"registry,omitempty"`
	Checksum string `json:"checksum,omitempty"`
	Format   string `json:"format,omitempty"`
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

// ComputeConfig declares compute constraints for an ML job.
type ComputeConfig struct {
	Runtime             string `json:"runtime,omitempty"`
	Precision           string `json:"precision,omitempty"`
	DistributedStrategy string `json:"distributed_strategy,omitempty"`
	MaxTokens           int    `json:"max_tokens,omitempty"`
	MaxBatchSize        int    `json:"max_batch_size,omitempty"`
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
		if req.GPU.ComputeCapability != "" {
			gpu["compute_capability"] = req.GPU.ComputeCapability
		}
		if req.GPU.Interconnect != "" {
			gpu["interconnect"] = req.GPU.Interconnect
		}
		res["gpu"] = gpu
	}
	if req.TPU != nil {
		tpu := make(map[string]any)
		if req.TPU.Type != "" {
			tpu["type"] = req.TPU.Type
		}
		if req.TPU.Topology != "" {
			tpu["topology"] = req.TPU.Topology
		}
		if req.TPU.ChipCount > 0 {
			tpu["chip_count"] = req.TPU.ChipCount
		}
		res["tpu"] = tpu
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
	if req.ShmSizeGB > 0 {
		res["shm_size_gb"] = req.ShmSizeGB
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
	if ref.Format != "" {
		m["format"] = ref.Format
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

// WithGPUFull returns an EnqueueOption that sets detailed GPU resource
// requirements including compute capability and interconnect.
func WithGPUFull(gpuType string, count int, memoryGB float64, computeCapability string, interconnect string) ojs.EnqueueOption {
	return WithResources(ResourceRequirements{
		GPU: &GPURequirements{
			Type:              gpuType,
			Count:             count,
			MemoryGB:          memoryGB,
			ComputeCapability: computeCapability,
			Interconnect:      interconnect,
		},
	})
}

// WithTPU returns an EnqueueOption that sets TPU resource requirements.
func WithTPU(tpuType string, topology string, chipCount int) ojs.EnqueueOption {
	return WithResources(ResourceRequirements{
		TPU: &TPURequirements{
			Type:      tpuType,
			Topology:  topology,
			ChipCount: chipCount,
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

// WithCompute returns an EnqueueOption that sets compute configuration
// in the job's meta.compute field.
func WithCompute(cfg ComputeConfig) ojs.EnqueueOption {
	compute := make(map[string]any)
	if cfg.Runtime != "" {
		compute["runtime"] = cfg.Runtime
	}
	if cfg.Precision != "" {
		compute["precision"] = cfg.Precision
	}
	if cfg.DistributedStrategy != "" {
		compute["distributed_strategy"] = cfg.DistributedStrategy
	}
	if cfg.MaxTokens > 0 {
		compute["max_tokens"] = cfg.MaxTokens
	}
	if cfg.MaxBatchSize > 0 {
		compute["max_batch_size"] = cfg.MaxBatchSize
	}
	return ojs.WithMeta(map[string]any{
		"compute": compute,
	})
}

// ValidateResources checks that resource requirements are logically consistent.
func ValidateResources(req ResourceRequirements) error {
	if req.GPU != nil {
		if req.GPU.Count < 0 {
			return fmt.Errorf("ml: gpu count must be non-negative, got %d", req.GPU.Count)
		}
		if req.GPU.MemoryGB < 0 {
			return fmt.Errorf("ml: gpu memory_gb must be non-negative, got %f", req.GPU.MemoryGB)
		}
		if req.GPU.MemoryGB > 0 && req.GPU.Count == 0 {
			return fmt.Errorf("ml: gpu memory_gb requires count > 0")
		}
		if req.GPU.Type != "" && req.GPU.Count == 0 {
			return fmt.Errorf("ml: gpu type requires count > 0")
		}
		if req.GPU.ComputeCapability != "" && req.GPU.Count == 0 {
			return fmt.Errorf("ml: gpu compute_capability requires count > 0")
		}
		if req.GPU.Interconnect != "" && req.GPU.Count < 2 {
			return fmt.Errorf("ml: gpu interconnect requires count >= 2")
		}
	}
	if req.TPU != nil {
		if req.TPU.ChipCount < 0 {
			return fmt.Errorf("ml: tpu chip_count must be non-negative, got %d", req.TPU.ChipCount)
		}
		if req.TPU.Topology != "" && req.TPU.Type == "" {
			return fmt.Errorf("ml: tpu topology requires type to be set")
		}
	}
	if req.CPU != nil && req.CPU.Cores < 0 {
		return fmt.Errorf("ml: cpu cores must be non-negative, got %d", req.CPU.Cores)
	}
	if req.MemoryGB < 0 {
		return fmt.Errorf("ml: memory_gb must be non-negative, got %f", req.MemoryGB)
	}
	if req.StorageGB < 0 {
		return fmt.Errorf("ml: storage_gb must be non-negative, got %f", req.StorageGB)
	}
	if req.ShmSizeGB < 0 {
		return fmt.Errorf("ml: shm_size_gb must be non-negative, got %f", req.ShmSizeGB)
	}
	return nil
}

// ValidateModel checks that a model reference is logically consistent.
func ValidateModel(ref ModelReference) error {
	if ref.Name == "" {
		return fmt.Errorf("ml: model name is required")
	}
	return nil
}

// ValidateCheckpoint checks that a checkpoint configuration is consistent.
func ValidateCheckpoint(cfg CheckpointConfig) error {
	if cfg.IntervalSec < 0 {
		return fmt.Errorf("ml: checkpoint interval_s must be non-negative, got %d", cfg.IntervalSec)
	}
	if cfg.MaxCheckpoints < 0 {
		return fmt.Errorf("ml: checkpoint max_checkpoints must be non-negative, got %d", cfg.MaxCheckpoints)
	}
	return nil
}
