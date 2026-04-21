package ml

import "testing"

// ValidateResources delegates the GPU and TPU blocks to functions that own
// them. Splitting a chain of early returns is only safe if the order is
// unchanged, so these cases each break rules in two groups and pin the winner.
func TestValidateResourcesFirstFailureOrder(t *testing.T) {
	cases := []struct {
		name string
		req  ResourceRequirements
		want string
	}{
		{
			name: "GPU block beats TPU block",
			req: ResourceRequirements{
				GPU: &GPURequirements{Count: -1},
				TPU: &TPURequirements{ChipCount: -1},
			},
			want: "ml: gpu count must be non-negative, got -1",
		},
		{
			name: "GPU block beats host memory",
			req: ResourceRequirements{
				GPU:      &GPURequirements{Type: "a100"},
				MemoryGB: -1,
			},
			want: "ml: gpu type requires count > 0",
		},
		{
			name: "TPU block beats CPU",
			req: ResourceRequirements{
				TPU: &TPURequirements{Topology: "4x4"},
				CPU: &CPURequirements{Cores: -2},
			},
			want: "ml: tpu topology requires type to be set",
		},
		{
			name: "CPU beats host memory",
			req: ResourceRequirements{
				CPU:      &CPURequirements{Cores: -2},
				MemoryGB: -1,
			},
			want: "ml: cpu cores must be non-negative, got -2",
		},
		{
			name: "memory beats storage",
			req:  ResourceRequirements{MemoryGB: -1, StorageGB: -1},
			want: "ml: memory_gb must be non-negative, got -1.000000",
		},
		{
			name: "storage beats shm",
			req:  ResourceRequirements{StorageGB: -1, ShmSizeGB: -1},
			want: "ml: storage_gb must be non-negative, got -1.000000",
		},
		{
			name: "gpu memory ordering within the GPU block",
			req:  ResourceRequirements{GPU: &GPURequirements{MemoryGB: -1, Type: "a100"}},
			want: "ml: gpu memory_gb must be non-negative, got -1.000000",
		},
		{
			name: "gpu interconnect needs two GPUs",
			req:  ResourceRequirements{GPU: &GPURequirements{Count: 1, Interconnect: "nvlink"}},
			want: "ml: gpu interconnect requires count >= 2",
		},
		{
			name: "gpu compute capability needs a GPU",
			req:  ResourceRequirements{GPU: &GPURequirements{ComputeCapability: "8.0"}},
			want: "ml: gpu compute_capability requires count > 0",
		},
		{
			name: "shm alone",
			req:  ResourceRequirements{ShmSizeGB: -1},
			want: "ml: shm_size_gb must be non-negative, got -1.000000",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateResources(tc.req)
			if err == nil {
				t.Fatalf("ValidateResources() = nil, want %q", tc.want)
			}
			if err.Error() != tc.want {
				t.Errorf("ValidateResources() = %q, want %q", err.Error(), tc.want)
			}
		})
	}
}

// A nil accelerator block means "none requested" and must stay valid.
func TestValidateResourcesNilBlocksAreValid(t *testing.T) {
	valid := []ResourceRequirements{
		{},
		{GPU: &GPURequirements{Count: 2, Type: "a100", MemoryGB: 40, Interconnect: "nvlink"}},
		{TPU: &TPURequirements{Type: "v5e", Topology: "4x4", ChipCount: 16}},
		{CPU: &CPURequirements{Cores: 8}, MemoryGB: 32, StorageGB: 100, ShmSizeGB: 2},
	}
	for i, req := range valid {
		if err := ValidateResources(req); err != nil {
			t.Errorf("case %d: ValidateResources() = %v, want nil", i, err)
		}
	}
	if err := validateGPURequirements(nil); err != nil {
		t.Errorf("validateGPURequirements(nil) = %v, want nil", err)
	}
	if err := validateTPURequirements(nil); err != nil {
		t.Errorf("validateTPURequirements(nil) = %v, want nil", err)
	}
}
