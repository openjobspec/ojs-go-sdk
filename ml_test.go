package ojs

import "testing"

func TestMLResourcesBuilder(t *testing.T) {
	res := NewMLResources().
		WithGPUType("nvidia-a100").
		WithGPUCount(2).
		WithGPUMemoryGB(40).
		WithMemoryGB(64).
		WithCPUCores(8).
		WithModelID("resnet50").
		WithModelVersion("1.0.0").
		WithRuntime("pytorch").
		WithAcceleratorRequired(true)

	if res.GPUType != "nvidia-a100" {
		t.Errorf("expected gpu_type=nvidia-a100, got %s", res.GPUType)
	}
	if res.GPUCount != 2 {
		t.Errorf("expected gpu_count=2, got %d", res.GPUCount)
	}
	if res.GPUMemoryGB != 40 {
		t.Errorf("expected gpu_memory_gb=40, got %f", res.GPUMemoryGB)
	}
	if res.MemoryGB != 64 {
		t.Errorf("expected memory_gb=64, got %f", res.MemoryGB)
	}
	if res.CPUCores != 8 {
		t.Errorf("expected cpu_cores=8, got %d", res.CPUCores)
	}
	if res.ModelID != "resnet50" {
		t.Errorf("expected model_id=resnet50, got %s", res.ModelID)
	}
	if res.ModelVersion != "1.0.0" {
		t.Errorf("expected model_version=1.0.0, got %s", res.ModelVersion)
	}
	if res.Runtime != "pytorch" {
		t.Errorf("expected runtime=pytorch, got %s", res.Runtime)
	}
	if !res.AcceleratorRequired {
		t.Error("expected accelerator_required=true")
	}
}

func TestWithMLResources(t *testing.T) {
	res := NewMLResources().
		WithGPUType("nvidia-h100").
		WithGPUCount(4).
		WithRuntime("vllm").
		WithAcceleratorRequired(true)

	cfg := resolveEnqueueConfig([]EnqueueOption{WithMLResources(res)})

	if cfg.meta == nil {
		t.Fatal("expected meta to be set")
	}

	ext, ok := cfg.meta["ext_ml_resources"]
	if !ok {
		t.Fatal("expected ext_ml_resources key in meta")
	}

	mlRes, ok := ext.(MLResources)
	if !ok {
		t.Fatal("expected ext_ml_resources to be MLResources type")
	}

	if mlRes.GPUType != "nvidia-h100" {
		t.Errorf("expected gpu_type=nvidia-h100, got %s", mlRes.GPUType)
	}
	if mlRes.GPUCount != 4 {
		t.Errorf("expected gpu_count=4, got %d", mlRes.GPUCount)
	}
	if mlRes.Runtime != "vllm" {
		t.Errorf("expected runtime=vllm, got %s", mlRes.Runtime)
	}
	if !mlRes.AcceleratorRequired {
		t.Error("expected accelerator_required=true")
	}
}

func TestWithMLResources_CombinedWithOtherOptions(t *testing.T) {
	res := NewMLResources().WithGPUCount(1).WithAcceleratorRequired(true)

	cfg := resolveEnqueueConfig([]EnqueueOption{
		WithQueue("ml-training"),
		WithPriority(50),
		WithTags("gpu", "training"),
		WithMLResources(res),
		WithMeta(map[string]any{"experiment": "exp-42"}),
	})

	if cfg.queue != "ml-training" {
		t.Errorf("expected queue=ml-training, got %s", cfg.queue)
	}
	if cfg.priority != 50 {
		t.Errorf("expected priority=50, got %d", cfg.priority)
	}
	if len(cfg.tags) != 2 {
		t.Errorf("expected 2 tags, got %d", len(cfg.tags))
	}
	if cfg.meta["experiment"] != "exp-42" {
		t.Errorf("expected experiment=exp-42, got %v", cfg.meta["experiment"])
	}
	if _, ok := cfg.meta["ext_ml_resources"]; !ok {
		t.Error("expected ext_ml_resources in meta")
	}
}

func TestWithMLResources_DefaultValues(t *testing.T) {
	res := NewMLResources()
	cfg := resolveEnqueueConfig([]EnqueueOption{WithMLResources(res)})

	ext := cfg.meta["ext_ml_resources"].(MLResources)
	if ext.GPUType != "" {
		t.Errorf("expected empty gpu_type, got %s", ext.GPUType)
	}
	if ext.GPUCount != 0 {
		t.Errorf("expected gpu_count=0, got %d", ext.GPUCount)
	}
	if ext.AcceleratorRequired {
		t.Error("expected accelerator_required=false")
	}
}
