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
		"GPUNvidiaT4":    "nvidia-t4",
		"GPUNvidiaL4":    "nvidia-l4",
		"GPUNvidiaV100":  "nvidia-v100",
		"GPUAmdMI250":    "amd-mi250",
		"GPUAmdMI300X":   "amd-mi300x",
		"GPUGoogleTPUv5": "google-tpu-v5",
	}
	actual := map[string]string{
		"GPUNvidiaA100":  GPUNvidiaA100,
		"GPUNvidiaH100":  GPUNvidiaH100,
		"GPUNvidiaT4":    GPUNvidiaT4,
		"GPUNvidiaL4":    GPUNvidiaL4,
		"GPUNvidiaV100":  GPUNvidiaV100,
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

func TestResourceRequirementsConstruction(t *testing.T) {
	req := ResourceRequirements{
		GPU: &GPURequirements{
			Type:     GPUNvidiaA100,
			Count:    2,
			MemoryGB: 80,
		},
		CPU:       &CPURequirements{Cores: 8},
		MemoryGB:  64,
		StorageGB: 200,
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
	if req.CPU.Cores != 8 {
		t.Errorf("CPU.Cores = %d, want 8", req.CPU.Cores)
	}
	if req.MemoryGB != 64 {
		t.Errorf("MemoryGB = %f, want 64", req.MemoryGB)
	}
	if req.StorageGB != 200 {
		t.Errorf("StorageGB = %f, want 200", req.StorageGB)
	}
}

func TestModelReferenceConstruction(t *testing.T) {
	ref := ModelReference{
		Name:     "resnet50",
		Version:  "1.0.0",
		Registry: "huggingface",
		Checksum: "sha256:abc123",
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

func TestWithResources(t *testing.T) {
	opt := WithResources(ResourceRequirements{
		GPU:       &GPURequirements{Type: GPUNvidiaA100, Count: 2, MemoryGB: 40},
		CPU:       &CPURequirements{Cores: 8},
		MemoryGB:  64,
		StorageGB: 200,
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
}

func TestWithModel(t *testing.T) {
	opt := WithModel(ModelReference{
		Name:     "llama-3",
		Version:  "70b",
		Registry: "huggingface",
		Checksum: "sha256:deadbeef",
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
}

func TestOptionsCompose(t *testing.T) {
	opts := []ojs.EnqueueOption{
		ojs.WithQueue("ml-training"),
		WithGPU(GPUNvidiaA100, 2, 80),
		WithModel(ModelReference{Name: "resnet50", Version: "1.0.0"}),
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
}

func TestResourceRequirementsJSON(t *testing.T) {
	req := ResourceRequirements{
		GPU:       &GPURequirements{Type: GPUNvidiaA100, Count: 2, MemoryGB: 80},
		CPU:       &CPURequirements{Cores: 8},
		MemoryGB:  64,
		StorageGB: 200,
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
	if decoded.MemoryGB != 64 {
		t.Errorf("decoded MemoryGB = %f, want 64", decoded.MemoryGB)
	}
}

func TestModelReferenceJSON(t *testing.T) {
	ref := ModelReference{
		Name:     "bert-base",
		Version:  "2.0",
		Registry: "huggingface",
		Checksum: "sha256:abc",
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
}
