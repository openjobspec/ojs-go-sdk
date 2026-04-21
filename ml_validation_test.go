package ojs

import "testing"

// MLResources.Validate was split into five rule groups. Splitting a chain of
// early returns is only safe if the evaluation order is unchanged: a request
// that breaks several rules must still report the same first failure. These
// cases each violate rules from two different groups and pin the winner.
func TestMLResourcesValidateFirstFailureOrder(t *testing.T) {
	cases := []struct {
		name string
		res  MLResources
		want string
	}{
		{
			name: "device quantities beat GPU dependency rules",
			res:  MLResources{GPUCount: -1, GPUType: "a100"},
			want: "ojs: gpu_count must be non-negative, got -1",
		},
		{
			name: "device quantities beat TPU rules",
			res:  MLResources{MemoryGB: -1, TPUChipCount: -4},
			want: "ojs: memory_gb must be non-negative, got -1.000000",
		},
		{
			name: "GPU dependency rules beat TPU rules",
			res:  MLResources{GPUType: "a100", TPUChipCount: -4},
			want: "ojs: gpu_type requires gpu_count > 0",
		},
		{
			name: "GPU dependency rules beat execution limits",
			res:  MLResources{GPUMemoryGB: 8, MaxTokens: -1},
			want: "ojs: gpu_memory_gb requires gpu_count > 0",
		},
		{
			name: "TPU rules beat execution limits",
			res:  MLResources{TPUChipCount: -4, MaxTokens: -1},
			want: "ojs: tpu_chip_count must be non-negative, got -4",
		},
		{
			name: "TPU topology rule beats execution limits",
			res:  MLResources{TPUTopology: "4x4", TimeoutSeconds: -1},
			want: "ojs: tpu_topology requires tpu_type to be set",
		},
		{
			name: "execution limits beat accelerator consistency",
			res:  MLResources{Accelerator: "tpu", GPUCount: 2, MaxBatchSize: -1},
			want: "ojs: max_batch_size must be non-negative, got -1",
		},
		{
			name: "accelerator consistency is last",
			res:  MLResources{Accelerator: "cpu", TPUChipCount: 4},
			want: "ojs: accelerator=cpu is incompatible with gpu_count or tpu_chip_count > 0",
		},
		{
			name: "gpu interconnect needs two GPUs",
			res:  MLResources{GPUCount: 1, GPUInterconnect: "nvlink"},
			want: "ojs: gpu_interconnect requires gpu_count >= 2",
		},
		{
			name: "compute capability needs a GPU",
			res:  MLResources{GPUComputeCapability: "8.0"},
			want: "ojs: gpu_compute_capability requires gpu_count > 0",
		},
		{
			name: "accelerator tpu rejects GPUs",
			res:  MLResources{Accelerator: "tpu", GPUCount: 2},
			want: "ojs: accelerator=tpu is incompatible with gpu_count > 0",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.res.Validate()
			if err == nil {
				t.Fatalf("Validate() = nil, want %q", tc.want)
			}
			if err.Error() != tc.want {
				t.Errorf("Validate() = %q, want %q", err.Error(), tc.want)
			}
		})
	}
}

func TestMLResourcesValidateAcceptsConsistentRequests(t *testing.T) {
	valid := []MLResources{
		{},
		{Accelerator: "gpu", GPUCount: 2, GPUType: "a100", GPUMemoryGB: 40, GPUInterconnect: "nvlink"},
		{Accelerator: "tpu", TPUType: "v5e", TPUTopology: "4x4", TPUChipCount: 16},
		{Accelerator: "cpu", CPUCores: 8, MemoryGB: 32, StorageGB: 100, ShmSizeGB: 2},
		{MaxTokens: 4096, MaxBatchSize: 32, TimeoutSeconds: 300},
	}
	for i, res := range valid {
		if err := res.Validate(); err != nil {
			t.Errorf("case %d: Validate() = %v, want nil", i, err)
		}
	}
}
