package ojs

import "testing"

func TestMLResourcesBuilder(t *testing.T) {
	res := NewMLResources().
		WithAccelerator("gpu").
		WithGPUType("nvidia-a100").
		WithGPUCount(2).
		WithGPUMemoryGB(40).
		WithGPUComputeCapability("8.0").
		WithGPUInterconnect("nvlink").
		WithMemoryGB(64).
		WithCPUCores(8).
		WithStorageGB(500).
		WithShmSizeGB(16).
		WithModelID("llama-3.1-70b").
		WithModelVersion("v1.0").
		WithModelProvider("huggingface").
		WithModelChecksum("sha256:abc123").
		WithModelFormat("safetensors").
		WithMaxTokens(4096).
		WithMaxBatchSize(32).
		WithTimeoutSeconds(300).
		WithPriorityClass("on-demand").
		WithRuntime("vllm").
		WithPrecision("fp16").
		WithDistributedStrategy("tensor_parallel")

	if res.Accelerator != "gpu" {
		t.Errorf("expected accelerator=gpu, got %s", res.Accelerator)
	}
	if res.GPUType != "nvidia-a100" {
		t.Errorf("expected gpu_type=nvidia-a100, got %s", res.GPUType)
	}
	if res.GPUCount != 2 {
		t.Errorf("expected gpu_count=2, got %d", res.GPUCount)
	}
	if res.GPUMemoryGB != 40 {
		t.Errorf("expected gpu_memory_gb=40, got %f", res.GPUMemoryGB)
	}
	if res.GPUComputeCapability != "8.0" {
		t.Errorf("expected gpu_compute_capability=8.0, got %s", res.GPUComputeCapability)
	}
	if res.GPUInterconnect != "nvlink" {
		t.Errorf("expected gpu_interconnect=nvlink, got %s", res.GPUInterconnect)
	}
	if res.MemoryGB != 64 {
		t.Errorf("expected memory_gb=64, got %f", res.MemoryGB)
	}
	if res.CPUCores != 8 {
		t.Errorf("expected cpu_cores=8, got %d", res.CPUCores)
	}
	if res.StorageGB != 500 {
		t.Errorf("expected storage_gb=500, got %f", res.StorageGB)
	}
	if res.ShmSizeGB != 16 {
		t.Errorf("expected shm_size_gb=16, got %f", res.ShmSizeGB)
	}
	if res.ModelID != "llama-3.1-70b" {
		t.Errorf("expected model_id=llama-3.1-70b, got %s", res.ModelID)
	}
	if res.ModelVersion != "v1.0" {
		t.Errorf("expected model_version=v1.0, got %s", res.ModelVersion)
	}
	if res.ModelProvider != "huggingface" {
		t.Errorf("expected model_provider=huggingface, got %s", res.ModelProvider)
	}
	if res.ModelChecksum != "sha256:abc123" {
		t.Errorf("expected model_checksum=sha256:abc123, got %s", res.ModelChecksum)
	}
	if res.ModelFormat != "safetensors" {
		t.Errorf("expected model_format=safetensors, got %s", res.ModelFormat)
	}
	if res.MaxTokens != 4096 {
		t.Errorf("expected max_tokens=4096, got %d", res.MaxTokens)
	}
	if res.MaxBatchSize != 32 {
		t.Errorf("expected max_batch_size=32, got %d", res.MaxBatchSize)
	}
	if res.TimeoutSeconds != 300 {
		t.Errorf("expected timeout_seconds=300, got %d", res.TimeoutSeconds)
	}
	if res.PriorityClass != "on-demand" {
		t.Errorf("expected priority_class=on-demand, got %s", res.PriorityClass)
	}
	if res.Runtime != "vllm" {
		t.Errorf("expected runtime=vllm, got %s", res.Runtime)
	}
	if res.Precision != "fp16" {
		t.Errorf("expected precision=fp16, got %s", res.Precision)
	}
	if res.DistributedStrategy != "tensor_parallel" {
		t.Errorf("expected distributed_strategy=tensor_parallel, got %s", res.DistributedStrategy)
	}
}

func TestMLResourcesTPUBuilder(t *testing.T) {
	res := NewMLResources().
		WithAccelerator("tpu").
		WithTPUType("v5e").
		WithTPUTopology("4x4").
		WithTPUChipCount(16).
		WithMemoryGB(256).
		WithRuntime("tensorflow").
		WithPrecision("bf16")

	if res.Accelerator != "tpu" {
		t.Errorf("expected accelerator=tpu, got %s", res.Accelerator)
	}
	if res.TPUType != "v5e" {
		t.Errorf("expected tpu_type=v5e, got %s", res.TPUType)
	}
	if res.TPUTopology != "4x4" {
		t.Errorf("expected tpu_topology=4x4, got %s", res.TPUTopology)
	}
	if res.TPUChipCount != 16 {
		t.Errorf("expected tpu_chip_count=16, got %d", res.TPUChipCount)
	}
	if res.Runtime != "tensorflow" {
		t.Errorf("expected runtime=tensorflow, got %s", res.Runtime)
	}
	if res.Precision != "bf16" {
		t.Errorf("expected precision=bf16, got %s", res.Precision)
	}
}

func TestMLResourcesValidate(t *testing.T) {
	tests := []struct {
		name    string
		res     MLResources
		wantErr bool
	}{
		{"valid full", NewMLResources().WithGPUType("nvidia-a100").WithGPUCount(2).WithGPUMemoryGB(40).WithMemoryGB(64).WithCPUCores(8), false},
		{"valid empty", NewMLResources(), false},
		{"valid cpu only", NewMLResources().WithCPUCores(16).WithMemoryGB(32), false},
		{"valid tpu", NewMLResources().WithAccelerator("tpu").WithTPUType("v5e").WithTPUTopology("2x4").WithTPUChipCount(8), false},
		{"valid gpu with compute capability", NewMLResources().WithGPUCount(4).WithGPUComputeCapability("9.0"), false},
		{"valid gpu with interconnect", NewMLResources().WithGPUCount(4).WithGPUInterconnect("nvlink"), false},
		{"negative gpu_count", MLResources{GPUCount: -1}, true},
		{"negative memory", MLResources{MemoryGB: -1}, true},
		{"negative cpu", MLResources{CPUCores: -1}, true},
		{"negative gpu_memory", MLResources{GPUMemoryGB: -1}, true},
		{"negative storage", MLResources{StorageGB: -1}, true},
		{"negative shm_size", MLResources{ShmSizeGB: -1}, true},
		{"gpu_memory without gpu_count", MLResources{GPUMemoryGB: 40}, true},
		{"gpu_type without gpu_count", MLResources{GPUType: "nvidia-a100"}, true},
		{"gpu_compute_capability without gpu_count", MLResources{GPUComputeCapability: "8.0"}, true},
		{"gpu_interconnect without enough gpus", MLResources{GPUCount: 1, GPUInterconnect: "nvlink"}, true},
		{"tpu_topology without tpu_type", MLResources{TPUTopology: "2x4"}, true},
		{"negative tpu_chip_count", MLResources{TPUChipCount: -1}, true},
		{"negative max_tokens", MLResources{MaxTokens: -1}, true},
		{"negative max_batch_size", MLResources{MaxBatchSize: -1}, true},
		{"negative timeout_seconds", MLResources{TimeoutSeconds: -1}, true},
		{"tpu accelerator with gpu_count", MLResources{Accelerator: "tpu", GPUCount: 2}, true},
		{"cpu accelerator with gpu_count", MLResources{Accelerator: "cpu", GPUCount: 1}, true},
		{"cpu accelerator with tpu_chip_count", MLResources{Accelerator: "cpu", TPUChipCount: 4}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.res.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestWithMLResources(t *testing.T) {
	res := NewMLResources().
		WithAccelerator("gpu").
		WithGPUType("nvidia-h100").
		WithGPUCount(4).
		WithGPUComputeCapability("9.0").
		WithModelProvider("huggingface").
		WithPriorityClass("on-demand").
		WithRuntime("vllm").
		WithPrecision("fp16")

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

	if mlRes.Accelerator != "gpu" {
		t.Errorf("expected accelerator=gpu, got %s", mlRes.Accelerator)
	}
	if mlRes.GPUType != "nvidia-h100" {
		t.Errorf("expected gpu_type=nvidia-h100, got %s", mlRes.GPUType)
	}
	if mlRes.GPUCount != 4 {
		t.Errorf("expected gpu_count=4, got %d", mlRes.GPUCount)
	}
	if mlRes.GPUComputeCapability != "9.0" {
		t.Errorf("expected gpu_compute_capability=9.0, got %s", mlRes.GPUComputeCapability)
	}
	if mlRes.ModelProvider != "huggingface" {
		t.Errorf("expected model_provider=huggingface, got %s", mlRes.ModelProvider)
	}
	if mlRes.PriorityClass != "on-demand" {
		t.Errorf("expected priority_class=on-demand, got %s", mlRes.PriorityClass)
	}
	if mlRes.Runtime != "vllm" {
		t.Errorf("expected runtime=vllm, got %s", mlRes.Runtime)
	}
	if mlRes.Precision != "fp16" {
		t.Errorf("expected precision=fp16, got %s", mlRes.Precision)
	}
}

func TestWithMLResources_CombinedWithOtherOptions(t *testing.T) {
	res := NewMLResources().WithGPUCount(1).WithAccelerator("gpu")

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
	if ext.Accelerator != "" {
		t.Errorf("expected empty accelerator, got %s", ext.Accelerator)
	}
	if ext.GPUCount != 0 {
		t.Errorf("expected gpu_count=0, got %d", ext.GPUCount)
	}
	if ext.PriorityClass != "" {
		t.Error("expected empty priority_class")
	}
	if ext.Runtime != "" {
		t.Error("expected empty runtime")
	}
	if ext.Precision != "" {
		t.Error("expected empty precision")
	}
}

func TestAffinityOption(t *testing.T) {
	aff := Affinity{
		Required: []AffinityRule{
			{Key: "region", Operator: AffinityIn, Values: []string{"us-east-1", "us-west-2"}},
			{Key: "compute_capability", Operator: AffinityGte, Values: []string{"8.0"}},
		},
		Preferred: []WeightedAffinityRule{
			{Key: "gpu_interconnect", Operator: AffinityIn, Values: []string{"nvlink"}, Weight: 80},
			{Key: "spot", Operator: AffinityNotIn, Values: []string{"true"}, Weight: 20},
		},
	}

	cfg := resolveEnqueueConfig([]EnqueueOption{WithAffinity(aff)})

	ext, ok := cfg.meta["ext_affinity"]
	if !ok {
		t.Fatal("expected ext_affinity in meta")
	}
	a, ok := ext.(Affinity)
	if !ok {
		t.Fatal("expected ext_affinity to be Affinity type")
	}
	if len(a.Required) != 2 {
		t.Errorf("expected 2 required rules, got %d", len(a.Required))
	}
	if a.Required[0].Key != "region" {
		t.Errorf("expected key=region, got %s", a.Required[0].Key)
	}
	if a.Required[0].Operator != AffinityIn {
		t.Errorf("expected operator=In, got %s", a.Required[0].Operator)
	}
	if a.Required[1].Operator != AffinityGte {
		t.Errorf("expected operator=Gte, got %s", a.Required[1].Operator)
	}
	if len(a.Preferred) != 2 {
		t.Errorf("expected 2 preferred rules, got %d", len(a.Preferred))
	}
	if a.Preferred[0].Weight != 80 {
		t.Errorf("expected weight=80, got %d", a.Preferred[0].Weight)
	}
}

func TestNodeSelectorOption(t *testing.T) {
	labels := map[string]string{
		"gpu_type":      "nvidia-a100",
		"region":        "us-east-1",
		"instance_type": "p4d.24xlarge",
	}

	cfg := resolveEnqueueConfig([]EnqueueOption{WithNodeSelector(labels)})

	ext, ok := cfg.meta["ext_node_selector"]
	if !ok {
		t.Fatal("expected ext_node_selector in meta")
	}
	sel, ok := ext.(map[string]string)
	if !ok {
		t.Fatal("expected ext_node_selector to be map[string]string")
	}
	if sel["gpu_type"] != "nvidia-a100" {
		t.Errorf("expected gpu_type=nvidia-a100, got %s", sel["gpu_type"])
	}
	if sel["region"] != "us-east-1" {
		t.Errorf("expected region=us-east-1, got %s", sel["region"])
	}
	if sel["instance_type"] != "p4d.24xlarge" {
		t.Errorf("expected instance_type=p4d.24xlarge, got %s", sel["instance_type"])
	}
}

func TestCheckpointingOption(t *testing.T) {
	cfg := resolveEnqueueConfig([]EnqueueOption{
		WithCheckpointing(CheckpointConfig{
			Enabled:        true,
			IntervalSec:    300,
			StorageURI:     "s3://my-bucket/checkpoints/",
			MaxCheckpoints: 3,
		}),
	})

	ext, ok := cfg.meta["ext_checkpoint"]
	if !ok {
		t.Fatal("expected ext_checkpoint in meta")
	}
	cp, ok := ext.(CheckpointConfig)
	if !ok {
		t.Fatal("expected CheckpointConfig type")
	}
	if !cp.Enabled {
		t.Error("expected enabled=true")
	}
	if cp.IntervalSec != 300 {
		t.Errorf("expected interval_s=300, got %d", cp.IntervalSec)
	}
	if cp.StorageURI != "s3://my-bucket/checkpoints/" {
		t.Errorf("unexpected storage_uri: %s", cp.StorageURI)
	}
}

func TestGetLastCheckpoint(t *testing.T) {
	job := Job{
		Meta: map[string]any{
			"last_checkpoint": map[string]any{
				"job_id":      "job-123",
				"epoch":       float64(42),
				"loss":        0.0234,
				"storage_key": "s3://bucket/checkpoints/job-123/epoch-42.pt",
				"created_at":  "2026-02-19T12:00:00Z",
			},
		},
	}

	cp, ok := GetLastCheckpoint(job)
	if !ok {
		t.Fatal("expected checkpoint to be found")
	}
	if cp.JobID != "job-123" {
		t.Errorf("expected job_id=job-123, got %s", cp.JobID)
	}
	if cp.Epoch != 42 {
		t.Errorf("expected epoch=42, got %d", cp.Epoch)
	}
	if cp.Loss != 0.0234 {
		t.Errorf("expected loss=0.0234, got %f", cp.Loss)
	}
	if cp.StorageKey != "s3://bucket/checkpoints/job-123/epoch-42.pt" {
		t.Errorf("unexpected storage_key: %s", cp.StorageKey)
	}
	if cp.CreatedAt != "2026-02-19T12:00:00Z" {
		t.Errorf("unexpected created_at: %s", cp.CreatedAt)
	}
}

func TestGetLastCheckpoint_Missing(t *testing.T) {
	job := Job{Meta: map[string]any{"other": "value"}}
	_, ok := GetLastCheckpoint(job)
	if ok {
		t.Error("expected no checkpoint")
	}

	jobNoMeta := Job{}
	_, ok = GetLastCheckpoint(jobNoMeta)
	if ok {
		t.Error("expected no checkpoint for nil meta")
	}
}

func TestPreemptionOption(t *testing.T) {
	cfg := resolveEnqueueConfig([]EnqueueOption{
		WithPreemptionPolicy(PreemptionPolicy{
			Preemptible:         true,
			GracePeriodSec:      60,
			CheckpointOnPreempt: true,
		}),
	})

	ext, ok := cfg.meta["ext_preemption"]
	if !ok {
		t.Fatal("expected ext_preemption in meta")
	}
	p, ok := ext.(PreemptionPolicy)
	if !ok {
		t.Fatal("expected PreemptionPolicy type")
	}
	if !p.Preemptible {
		t.Error("expected preemptible=true")
	}
	if p.GracePeriodSec != 60 {
		t.Errorf("expected grace_period_s=60, got %d", p.GracePeriodSec)
	}
	if !p.CheckpointOnPreempt {
		t.Error("expected checkpoint_on_preempt=true")
	}
}

func TestResourceReservationOption(t *testing.T) {
	cfg := resolveEnqueueConfig([]EnqueueOption{
		WithResourceReservation(ResourceReservation{
			ReservationID:  "res-gpu-cluster-001",
			TimeoutSeconds: 3600,
		}),
	})

	ext, ok := cfg.meta["ext_reservation"]
	if !ok {
		t.Fatal("expected ext_reservation in meta")
	}
	r, ok := ext.(ResourceReservation)
	if !ok {
		t.Fatal("expected ResourceReservation type")
	}
	if r.ReservationID != "res-gpu-cluster-001" {
		t.Errorf("expected reservation_id=res-gpu-cluster-001, got %s", r.ReservationID)
	}
	if r.TimeoutSeconds != 3600 {
		t.Errorf("expected timeout_seconds=3600, got %d", r.TimeoutSeconds)
	}
}

func TestWorkerCapabilitiesOption(t *testing.T) {
	cap := WorkerCapabilities{
		Accelerator: "gpu",
		GPU: &GPUCapability{
			Count:             4,
			Type:              "nvidia-a100",
			MemoryGB:          80,
			ComputeCapability: "8.0",
			Interconnect:      "nvlink",
		},
		CPUCores:  32,
		MemoryGB:  256,
		StorageGB: 1000,
		ShmSizeGB: 64,
		Models: []LoadedModel{
			{ModelID: "llama-3.1-70b", ModelVersion: "v2.1", ModelFormat: "safetensors"},
		},
		Runtimes: []string{"vllm", "pytorch"},
		Labels:   map[string]string{"region": "us-east-1", "cluster": "ml-prod"},
	}

	cfg := resolveWorkerConfig([]WorkerOption{WithWorkerCapabilities(cap)})

	if cfg.capabilities == nil {
		t.Fatal("expected capabilities to be set")
	}
	if cfg.capabilities.Accelerator != "gpu" {
		t.Errorf("expected accelerator=gpu, got %s", cfg.capabilities.Accelerator)
	}
	if cfg.capabilities.GPU.Count != 4 {
		t.Errorf("expected gpu count=4, got %d", cfg.capabilities.GPU.Count)
	}
	if cfg.capabilities.GPU.ComputeCapability != "8.0" {
		t.Errorf("expected compute_capability=8.0, got %s", cfg.capabilities.GPU.ComputeCapability)
	}
	if cfg.capabilities.GPU.Interconnect != "nvlink" {
		t.Errorf("expected interconnect=nvlink, got %s", cfg.capabilities.GPU.Interconnect)
	}
	if cfg.capabilities.CPUCores != 32 {
		t.Errorf("expected cpu_cores=32, got %d", cfg.capabilities.CPUCores)
	}
	if cfg.capabilities.ShmSizeGB != 64 {
		t.Errorf("expected shm_size_gb=64, got %f", cfg.capabilities.ShmSizeGB)
	}
	if len(cfg.capabilities.Models) != 1 {
		t.Fatalf("expected 1 loaded model, got %d", len(cfg.capabilities.Models))
	}
	if cfg.capabilities.Models[0].ModelID != "llama-3.1-70b" {
		t.Errorf("expected model_id=llama-3.1-70b, got %s", cfg.capabilities.Models[0].ModelID)
	}
	if len(cfg.capabilities.Runtimes) != 2 {
		t.Errorf("expected 2 runtimes, got %d", len(cfg.capabilities.Runtimes))
	}
	if cfg.capabilities.Labels["region"] != "us-east-1" {
		t.Errorf("unexpected region label: %s", cfg.capabilities.Labels["region"])
	}
}

func TestWorkerCapabilitiesTPU(t *testing.T) {
	cap := WorkerCapabilities{
		Accelerator: "tpu",
		TPU: &TPUCapability{
			Type:      "v5e",
			Topology:  "4x4",
			ChipCount: 16,
		},
		CPUCores: 96,
		MemoryGB: 256,
	}

	cfg := resolveWorkerConfig([]WorkerOption{WithWorkerCapabilities(cap)})

	if cfg.capabilities.Accelerator != "tpu" {
		t.Errorf("expected accelerator=tpu, got %s", cfg.capabilities.Accelerator)
	}
	if cfg.capabilities.TPU == nil {
		t.Fatal("expected TPU capabilities to be set")
	}
	if cfg.capabilities.TPU.Type != "v5e" {
		t.Errorf("expected tpu type=v5e, got %s", cfg.capabilities.TPU.Type)
	}
	if cfg.capabilities.TPU.Topology != "4x4" {
		t.Errorf("expected tpu topology=4x4, got %s", cfg.capabilities.TPU.Topology)
	}
	if cfg.capabilities.TPU.ChipCount != 16 {
		t.Errorf("expected tpu chip_count=16, got %d", cfg.capabilities.TPU.ChipCount)
	}
}
