package ml

import (
	"encoding/json"
	"testing"

	ojs "github.com/openjobspec/ojs-go-sdk"
)

func TestGPUTypeConstants(t *testing.T) {
	expected := map[string]string{
		"GPUNvidiaA100":  "nvidia-a100",
		"GPUNvidiaH100":  "nvidia-h100",
		"GPUNvidiaH200":  "nvidia-h200",
		"GPUNvidiaT4":    "nvidia-t4",
		"GPUNvidiaL4":    "nvidia-l4",
		"GPUNvidiaL40S":  "nvidia-l40s",
		"GPUNvidiaV100":  "nvidia-v100",
		"GPUNvidiaA10G":  "nvidia-a10g",
		"GPUNvidiaB200":  "nvidia-b200",
		"GPUAmdMI250":    "amd-mi250",
		"GPUAmdMI300X":   "amd-mi300x",
		"GPUGoogleTPUv5": "google-tpu-v5",
	}
	actual := map[string]string{
		"GPUNvidiaA100":  GPUNvidiaA100,
		"GPUNvidiaH100":  GPUNvidiaH100,
		"GPUNvidiaH200":  GPUNvidiaH200,
		"GPUNvidiaT4":    GPUNvidiaT4,
		"GPUNvidiaL4":    GPUNvidiaL4,
		"GPUNvidiaL40S":  GPUNvidiaL40S,
		"GPUNvidiaV100":  GPUNvidiaV100,
		"GPUNvidiaA10G":  GPUNvidiaA10G,
		"GPUNvidiaB200":  GPUNvidiaB200,
		"GPUAmdMI250":    GPUAmdMI250,
		"GPUAmdMI300X":   GPUAmdMI300X,
		"GPUGoogleTPUv5": GPUGoogleTPUv5,
	}
	for name, want := range expected {
		if got := actual[name]; got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestTPUTypeConstants(t *testing.T) {
	if TPUv4 != "v4" {
		t.Errorf("TPUv4 = %q, want v4", TPUv4)
	}
	if TPUv5e != "v5e" {
		t.Errorf("TPUv5e = %q, want v5e", TPUv5e)
	}
	if TPUv5p != "v5p" {
		t.Errorf("TPUv5p = %q, want v5p", TPUv5p)
	}
	if TPUv6e != "v6e" {
		t.Errorf("TPUv6e = %q, want v6e", TPUv6e)
	}
}

func TestPrecisionConstants(t *testing.T) {
	if PrecisionFP32 != "fp32" {
		t.Errorf("PrecisionFP32 = %q, want fp32", PrecisionFP32)
	}
	if PrecisionBF16 != "bf16" {
		t.Errorf("PrecisionBF16 = %q, want bf16", PrecisionBF16)
	}
	if PrecisionFP8 != "fp8" {
		t.Errorf("PrecisionFP8 = %q, want fp8", PrecisionFP8)
	}
	if PrecisionINT4 != "int4" {
		t.Errorf("PrecisionINT4 = %q, want int4", PrecisionINT4)
	}
}

func TestRuntimeConstants(t *testing.T) {
	if RuntimeVLLM != "vllm" {
		t.Errorf("RuntimeVLLM = %q, want vllm", RuntimeVLLM)
	}
	if RuntimeTGI != "tgi" {
		t.Errorf("RuntimeTGI = %q, want tgi", RuntimeTGI)
	}
	if RuntimePyTorch != "pytorch" {
		t.Errorf("RuntimePyTorch = %q, want pytorch", RuntimePyTorch)
	}
}

func TestResourceRequirementsConstruction(t *testing.T) {
	req := ResourceRequirements{
		GPU: &GPURequirements{
			Type:              GPUNvidiaA100,
			Count:             2,
			MemoryGB:          80,
			ComputeCapability: "8.0",
			Interconnect:      InterconnectNVLink,
		},
		CPU:       &CPURequirements{Cores: 8},
		MemoryGB:  64,
		StorageGB: 200,
		ShmSizeGB: 16,
	}

	if req.GPU.Type != "nvidia-a100" {
		t.Errorf("GPU.Type = %q, want nvidia-a100", req.GPU.Type)
	}
	if req.GPU.Count != 2 {
		t.Errorf("GPU.Count = %d, want 2", req.GPU.Count)
	}
	if req.GPU.MemoryGB != 80 {
		t.Errorf("GPU.MemoryGB = %f, want 80", req.GPU.MemoryGB)
	}
	if req.GPU.ComputeCapability != "8.0" {
		t.Errorf("GPU.ComputeCapability = %q, want 8.0", req.GPU.ComputeCapability)
	}
	if req.GPU.Interconnect != "nvlink" {
		t.Errorf("GPU.Interconnect = %q, want nvlink", req.GPU.Interconnect)
	}
	if req.CPU.Cores != 8 {
		t.Errorf("CPU.Cores = %d, want 8", req.CPU.Cores)
	}
	if req.MemoryGB != 64 {
		t.Errorf("MemoryGB = %f, want 64", req.MemoryGB)
	}
	if req.StorageGB != 200 {
		t.Errorf("StorageGB = %f, want 200", req.StorageGB)
	}
	if req.ShmSizeGB != 16 {
		t.Errorf("ShmSizeGB = %f, want 16", req.ShmSizeGB)
	}
}

func TestTPURequirementsConstruction(t *testing.T) {
	req := ResourceRequirements{
		TPU: &TPURequirements{
			Type:      TPUv5e,
			Topology:  "4x4",
			ChipCount: 16,
		},
		MemoryGB: 256,
	}

	if req.TPU.Type != "v5e" {
		t.Errorf("TPU.Type = %q, want v5e", req.TPU.Type)
	}
	if req.TPU.Topology != "4x4" {
		t.Errorf("TPU.Topology = %q, want 4x4", req.TPU.Topology)
	}
	if req.TPU.ChipCount != 16 {
		t.Errorf("TPU.ChipCount = %d, want 16", req.TPU.ChipCount)
	}
}

func TestModelReferenceConstruction(t *testing.T) {
	ref := ModelReference{
		Name:     "resnet50",
		Version:  "1.0.0",
		Registry: "huggingface",
		Checksum: "sha256:abc123",
		Format:   "safetensors",
	}

	if ref.Name != "resnet50" {
		t.Errorf("Name = %q, want resnet50", ref.Name)
	}
	if ref.Version != "1.0.0" {
		t.Errorf("Version = %q, want 1.0.0", ref.Version)
	}
	if ref.Registry != "huggingface" {
		t.Errorf("Registry = %q, want huggingface", ref.Registry)
	}
	if ref.Checksum != "sha256:abc123" {
		t.Errorf("Checksum = %q, want sha256:abc123", ref.Checksum)
	}
	if ref.Format != "safetensors" {
		t.Errorf("Format = %q, want safetensors", ref.Format)
	}
}

func TestWithGPU(t *testing.T) {
	opt := WithGPU(GPUNvidiaH100, 4, 80)
	cfg := ojs.ResolveTestEnqueueConfig([]ojs.EnqueueOption{opt})

	res, ok := cfg.Meta["resources"].(map[string]any)
	if !ok {
		t.Fatal("expected resources key in meta")
	}
	gpu, ok := res["gpu"].(map[string]any)
	if !ok {
		t.Fatal("expected gpu key in resources")
	}
	if gpu["type"] != GPUNvidiaH100 {
		t.Errorf("gpu type = %v, want %s", gpu["type"], GPUNvidiaH100)
	}
	if gpu["count"] != 4 {
		t.Errorf("gpu count = %v, want 4", gpu["count"])
	}
	if gpu["memory_gb"] != 80.0 {
		t.Errorf("gpu memory_gb = %v, want 80", gpu["memory_gb"])
	}
}

func TestWithGPUFull(t *testing.T) {
	opt := WithGPUFull(GPUNvidiaH100, 8, 80, "9.0", InterconnectNVLink)
	cfg := ojs.ResolveTestEnqueueConfig([]ojs.EnqueueOption{opt})

	res, ok := cfg.Meta["resources"].(map[string]any)
	if !ok {
		t.Fatal("expected resources key in meta")
	}
	gpu, ok := res["gpu"].(map[string]any)
	if !ok {
		t.Fatal("expected gpu key in resources")
	}
	if gpu["type"] != GPUNvidiaH100 {
		t.Errorf("gpu type = %v, want %s", gpu["type"], GPUNvidiaH100)
	}
	if gpu["count"] != 8 {
		t.Errorf("gpu count = %v, want 8", gpu["count"])
	}
	if gpu["compute_capability"] != "9.0" {
		t.Errorf("gpu compute_capability = %v, want 9.0", gpu["compute_capability"])
	}
	if gpu["interconnect"] != "nvlink" {
		t.Errorf("gpu interconnect = %v, want nvlink", gpu["interconnect"])
	}
}

func TestWithTPU(t *testing.T) {
	opt := WithTPU(TPUv5e, "4x4", 16)
	cfg := ojs.ResolveTestEnqueueConfig([]ojs.EnqueueOption{opt})

	res, ok := cfg.Meta["resources"].(map[string]any)
	if !ok {
		t.Fatal("expected resources key in meta")
	}
	tpu, ok := res["tpu"].(map[string]any)
	if !ok {
		t.Fatal("expected tpu key in resources")
	}
	if tpu["type"] != TPUv5e {
		t.Errorf("tpu type = %v, want %s", tpu["type"], TPUv5e)
	}
	if tpu["topology"] != "4x4" {
		t.Errorf("tpu topology = %v, want 4x4", tpu["topology"])
	}
	if tpu["chip_count"] != 16 {
		t.Errorf("tpu chip_count = %v, want 16", tpu["chip_count"])
	}
}

func TestWithResources(t *testing.T) {
	opt := WithResources(ResourceRequirements{
		GPU:       &GPURequirements{Type: GPUNvidiaA100, Count: 2, MemoryGB: 40, ComputeCapability: "8.0"},
		CPU:       &CPURequirements{Cores: 8},
		MemoryGB:  64,
		StorageGB: 200,
		ShmSizeGB: 16,
	})
	cfg := ojs.ResolveTestEnqueueConfig([]ojs.EnqueueOption{opt})

	res, ok := cfg.Meta["resources"].(map[string]any)
	if !ok {
		t.Fatal("expected resources in meta")
	}
	gpu, ok := res["gpu"].(map[string]any)
	if !ok {
		t.Fatal("expected gpu in resources")
	}
	if gpu["type"] != GPUNvidiaA100 {
		t.Errorf("gpu type = %v, want nvidia-a100", gpu["type"])
	}
	if gpu["compute_capability"] != "8.0" {
		t.Errorf("gpu compute_capability = %v, want 8.0", gpu["compute_capability"])
	}
	cpu, ok := res["cpu"].(map[string]any)
	if !ok {
		t.Fatal("expected cpu in resources")
	}
	if cpu["cores"] != 8 {
		t.Errorf("cpu cores = %v, want 8", cpu["cores"])
	}
	if res["memory_gb"] != 64.0 {
		t.Errorf("memory_gb = %v, want 64", res["memory_gb"])
	}
	if res["storage_gb"] != 200.0 {
		t.Errorf("storage_gb = %v, want 200", res["storage_gb"])
	}
	if res["shm_size_gb"] != 16.0 {
		t.Errorf("shm_size_gb = %v, want 16", res["shm_size_gb"])
	}
}

func TestWithModel(t *testing.T) {
	opt := WithModel(ModelReference{
		Name:     "llama-3",
		Version:  "70b",
		Registry: "huggingface",
		Checksum: "sha256:deadbeef",
		Format:   "safetensors",
	})
	cfg := ojs.ResolveTestEnqueueConfig([]ojs.EnqueueOption{opt})

	model, ok := cfg.Meta["model"].(map[string]any)
	if !ok {
		t.Fatal("expected model key in meta")
	}
	if model["name"] != "llama-3" {
		t.Errorf("model name = %v, want llama-3", model["name"])
	}
	if model["version"] != "70b" {
		t.Errorf("model version = %v, want 70b", model["version"])
	}
	if model["registry"] != "huggingface" {
		t.Errorf("model registry = %v, want huggingface", model["registry"])
	}
	if model["checksum"] != "sha256:deadbeef" {
		t.Errorf("model checksum = %v, want sha256:deadbeef", model["checksum"])
	}
	if model["format"] != "safetensors" {
		t.Errorf("model format = %v, want safetensors", model["format"])
	}
}

func TestOptionsCompose(t *testing.T) {
	opts := []ojs.EnqueueOption{
		ojs.WithQueue("ml-training"),
		WithGPU(GPUNvidiaA100, 2, 80),
		WithModel(ModelReference{Name: "resnet50", Version: "1.0.0", Format: "safetensors"}),
		WithCompute(ComputeConfig{Runtime: RuntimePyTorch, Precision: PrecisionBF16, DistributedStrategy: StrategyFSDP}),
	}
	cfg := ojs.ResolveTestEnqueueConfig(opts)

	if cfg.Queue != "ml-training" {
		t.Errorf("queue = %q, want ml-training", cfg.Queue)
	}
	if _, ok := cfg.Meta["resources"]; !ok {
		t.Error("expected resources in meta")
	}
	if _, ok := cfg.Meta["model"]; !ok {
		t.Error("expected model in meta")
	}
	if _, ok := cfg.Meta["compute"]; !ok {
		t.Error("expected compute in meta")
	}
}

func TestResourceRequirementsJSON(t *testing.T) {
	req := ResourceRequirements{
		GPU: &GPURequirements{
			Type:              GPUNvidiaA100,
			Count:             2,
			MemoryGB:          80,
			ComputeCapability: "8.0",
			Interconnect:      InterconnectNVLink,
		},
		CPU:       &CPURequirements{Cores: 8},
		MemoryGB:  64,
		StorageGB: 200,
		ShmSizeGB: 16,
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded ResourceRequirements
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.GPU.Type != GPUNvidiaA100 {
		t.Errorf("decoded GPU.Type = %q, want nvidia-a100", decoded.GPU.Type)
	}
	if decoded.GPU.Count != 2 {
		t.Errorf("decoded GPU.Count = %d, want 2", decoded.GPU.Count)
	}
	if decoded.GPU.ComputeCapability != "8.0" {
		t.Errorf("decoded GPU.ComputeCapability = %q, want 8.0", decoded.GPU.ComputeCapability)
	}
	if decoded.GPU.Interconnect != "nvlink" {
		t.Errorf("decoded GPU.Interconnect = %q, want nvlink", decoded.GPU.Interconnect)
	}
	if decoded.MemoryGB != 64 {
		t.Errorf("decoded MemoryGB = %f, want 64", decoded.MemoryGB)
	}
	if decoded.ShmSizeGB != 16 {
		t.Errorf("decoded ShmSizeGB = %f, want 16", decoded.ShmSizeGB)
	}
}

func TestModelReferenceJSON(t *testing.T) {
	ref := ModelReference{
		Name:     "bert-base",
		Version:  "2.0",
		Registry: "huggingface",
		Checksum: "sha256:abc",
		Format:   "safetensors",
	}
	data, err := json.Marshal(ref)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var decoded ModelReference
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if decoded.Name != "bert-base" {
		t.Errorf("decoded Name = %q, want bert-base", decoded.Name)
	}
	if decoded.Version != "2.0" {
		t.Errorf("decoded Version = %q, want 2.0", decoded.Version)
	}
	if decoded.Format != "safetensors" {
		t.Errorf("decoded Format = %q, want safetensors", decoded.Format)
	}
}

func TestWithCheckpoint(t *testing.T) {
	opt := WithCheckpoint(CheckpointConfig{
		Enabled:        true,
		IntervalSec:    300,
		StorageURI:     "s3://my-bucket/checkpoints/",
		MaxCheckpoints: 3,
	})
	cfg := ojs.ResolveTestEnqueueConfig([]ojs.EnqueueOption{opt})

	ckpt, ok := cfg.Meta["checkpoint"].(map[string]any)
	if !ok {
		t.Fatal("expected checkpoint in meta")
	}
	if ckpt["enabled"] != true {
		t.Error("expected checkpoint enabled=true")
	}
	if ckpt["interval_s"] != 300 {
		t.Errorf("interval_s = %v, want 300", ckpt["interval_s"])
	}
	if ckpt["storage_uri"] != "s3://my-bucket/checkpoints/" {
		t.Errorf("storage_uri = %v, want s3://my-bucket/checkpoints/", ckpt["storage_uri"])
	}
}

func TestWithPreemption(t *testing.T) {
	opt := WithPreemption(PreemptionConfig{
		Preemptible:         true,
		GracePeriodSec:      60,
		CheckpointOnPreempt: true,
	})
	cfg := ojs.ResolveTestEnqueueConfig([]ojs.EnqueueOption{opt})

	p, ok := cfg.Meta["preemption"].(map[string]any)
	if !ok {
		t.Fatal("expected preemption in meta")
	}
	if p["preemptible"] != true {
		t.Error("expected preemptible=true")
	}
	if p["grace_period_s"] != 60 {
		t.Errorf("grace_period_s = %v, want 60", p["grace_period_s"])
	}
	if p["checkpoint_on_preempt"] != true {
		t.Error("expected checkpoint_on_preempt=true")
	}
}

func TestWithCompute(t *testing.T) {
	opt := WithCompute(ComputeConfig{
		Runtime:             RuntimeVLLM,
		Precision:           PrecisionFP16,
		DistributedStrategy: StrategyTensorParallel,
		MaxTokens:           4096,
		MaxBatchSize:        64,
	})
	cfg := ojs.ResolveTestEnqueueConfig([]ojs.EnqueueOption{opt})

	compute, ok := cfg.Meta["compute"].(map[string]any)
	if !ok {
		t.Fatal("expected compute in meta")
	}
	if compute["runtime"] != "vllm" {
		t.Errorf("runtime = %v, want vllm", compute["runtime"])
	}
	if compute["precision"] != "fp16" {
		t.Errorf("precision = %v, want fp16", compute["precision"])
	}
	if compute["distributed_strategy"] != "tensor_parallel" {
		t.Errorf("distributed_strategy = %v, want tensor_parallel", compute["distributed_strategy"])
	}
	if compute["max_tokens"] != 4096 {
		t.Errorf("max_tokens = %v, want 4096", compute["max_tokens"])
	}
	if compute["max_batch_size"] != 64 {
		t.Errorf("max_batch_size = %v, want 64", compute["max_batch_size"])
	}
}

func TestEmptyResourceRequirements(t *testing.T) {
	opt := WithResources(ResourceRequirements{})
	cfg := ojs.ResolveTestEnqueueConfig([]ojs.EnqueueOption{opt})

	res, ok := cfg.Meta["resources"].(map[string]any)
	if !ok {
		t.Fatal("expected resources in meta")
	}
	if _, hasGPU := res["gpu"]; hasGPU {
		t.Error("expected no gpu key for empty requirements")
	}
	if _, hasCPU := res["cpu"]; hasCPU {
		t.Error("expected no cpu key for empty requirements")
	}
	if _, hasTPU := res["tpu"]; hasTPU {
		t.Error("expected no tpu key for empty requirements")
	}
}

func TestValidateResources(t *testing.T) {
	tests := []struct {
		name    string
		req     ResourceRequirements
		wantErr bool
	}{
		{"valid full GPU", ResourceRequirements{GPU: &GPURequirements{Type: GPUNvidiaA100, Count: 2, MemoryGB: 80, ComputeCapability: "8.0"}}, false},
		{"valid empty", ResourceRequirements{}, false},
		{"valid CPU only", ResourceRequirements{CPU: &CPURequirements{Cores: 16}, MemoryGB: 32}, false},
		{"valid TPU", ResourceRequirements{TPU: &TPURequirements{Type: TPUv5e, Topology: "4x4", ChipCount: 16}}, false},
		{"valid multi-GPU with interconnect", ResourceRequirements{GPU: &GPURequirements{Count: 4, Interconnect: InterconnectNVLink}}, false},
		{"negative gpu count", ResourceRequirements{GPU: &GPURequirements{Count: -1}}, true},
		{"negative gpu memory", ResourceRequirements{GPU: &GPURequirements{Count: 1, MemoryGB: -1}}, true},
		{"gpu memory without count", ResourceRequirements{GPU: &GPURequirements{MemoryGB: 80}}, true},
		{"gpu type without count", ResourceRequirements{GPU: &GPURequirements{Type: GPUNvidiaA100}}, true},
		{"gpu compute_capability without count", ResourceRequirements{GPU: &GPURequirements{ComputeCapability: "8.0"}}, true},
		{"gpu interconnect with count=1", ResourceRequirements{GPU: &GPURequirements{Count: 1, Interconnect: InterconnectNVLink}}, true},
		{"negative tpu chip_count", ResourceRequirements{TPU: &TPURequirements{ChipCount: -1}}, true},
		{"tpu topology without type", ResourceRequirements{TPU: &TPURequirements{Topology: "2x4"}}, true},
		{"negative cpu cores", ResourceRequirements{CPU: &CPURequirements{Cores: -1}}, true},
		{"negative memory_gb", ResourceRequirements{MemoryGB: -1}, true},
		{"negative storage_gb", ResourceRequirements{StorageGB: -1}, true},
		{"negative shm_size_gb", ResourceRequirements{ShmSizeGB: -1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateResources(tt.req)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateResources() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateModel(t *testing.T) {
	tests := []struct {
		name    string
		ref     ModelReference
		wantErr bool
	}{
		{"valid full", ModelReference{Name: "resnet50", Version: "1.0.0", Registry: "huggingface"}, false},
		{"valid name only", ModelReference{Name: "bert-base"}, false},
		{"empty name", ModelReference{}, true},
		{"empty name with version", ModelReference{Version: "1.0"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateModel(tt.ref)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateModel() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestValidateCheckpoint(t *testing.T) {
	tests := []struct {
		name    string
		cfg     CheckpointConfig
		wantErr bool
	}{
		{"valid enabled", CheckpointConfig{Enabled: true, IntervalSec: 300, MaxCheckpoints: 3}, false},
		{"valid disabled", CheckpointConfig{}, false},
		{"valid zero interval", CheckpointConfig{Enabled: true, IntervalSec: 0}, false},
		{"negative interval", CheckpointConfig{Enabled: true, IntervalSec: -1}, true},
		{"negative max_checkpoints", CheckpointConfig{Enabled: true, MaxCheckpoints: -1}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateCheckpoint(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateCheckpoint() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWithModelMinimal(t *testing.T) {
	opt := WithModel(ModelReference{Name: "bert-base"})
	cfg := ojs.ResolveTestEnqueueConfig([]ojs.EnqueueOption{opt})

	model, ok := cfg.Meta["model"].(map[string]any)
	if !ok {
		t.Fatal("expected model key in meta")
	}
	if model["name"] != "bert-base" {
		t.Errorf("model name = %v, want bert-base", model["name"])
	}
	if _, hasVersion := model["version"]; hasVersion {
		t.Error("expected no version key for minimal model ref")
	}
	if _, hasRegistry := model["registry"]; hasRegistry {
		t.Error("expected no registry key for minimal model ref")
	}
}

func TestWithCheckpointDisabled(t *testing.T) {
	opt := WithCheckpoint(CheckpointConfig{Enabled: false})
	cfg := ojs.ResolveTestEnqueueConfig([]ojs.EnqueueOption{opt})

	ckpt, ok := cfg.Meta["checkpoint"].(map[string]any)
	if !ok {
		t.Fatal("expected checkpoint in meta")
	}
	if ckpt["enabled"] != false {
		t.Error("expected checkpoint enabled=false")
	}
	if _, hasInterval := ckpt["interval_s"]; hasInterval {
		t.Error("expected no interval_s when not set")
	}
}

func TestWithGPUZeroMemory(t *testing.T) {
	opt := WithGPU(GPUNvidiaT4, 1, 0)
	cfg := ojs.ResolveTestEnqueueConfig([]ojs.EnqueueOption{opt})

	res, ok := cfg.Meta["resources"].(map[string]any)
	if !ok {
		t.Fatal("expected resources in meta")
	}
	gpu, ok := res["gpu"].(map[string]any)
	if !ok {
		t.Fatal("expected gpu in resources")
	}
	if gpu["type"] != GPUNvidiaT4 {
		t.Errorf("gpu type = %v, want %s", gpu["type"], GPUNvidiaT4)
	}
	if _, hasMemory := gpu["memory_gb"]; hasMemory {
		t.Error("expected no memory_gb when set to 0")
	}
}
