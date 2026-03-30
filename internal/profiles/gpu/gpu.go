/*
 * Copyright The Kubernetes Authors.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package gpu

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	resourceapi "k8s.io/api/resource/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/dynamic-resource-allocation/resourceslice"
	"k8s.io/utils/ptr"
	cdiapi "tags.cncf.io/container-device-interface/pkg/cdi"
	cdispec "tags.cncf.io/container-device-interface/specs-go"

	configapi "sigs.k8s.io/dra-example-driver/api/example.com/resource/gpu/v1alpha1"
	"sigs.k8s.io/dra-example-driver/internal/profiles"
)

const ProfileName = "gpu"

type Profile struct {
	nodeName string
	numGPUs  int
}

func NewProfile(nodeName string, numGPUs int) Profile {
	return Profile{
		nodeName: nodeName,
		numGPUs:  numGPUs,
	}
}

const vGPUsPerGPU = 2

func (p Profile) EnumerateDevices() (resourceslice.DriverResources, error) {
	seed := p.nodeName
	uuids := generateUUIDs(seed, p.numGPUs)
	//mdevUUIDs := generateMDevUUIDs(seed, p.numGPUs*vGPUsPerGPU)

	var devices []resourceapi.Device

	// pGPU devices (passthrough) — identified by pciBusID
	for i, uuid := range uuids {
		pciBusID := fmt.Sprintf("0000:00:%02d.0", i+1)
		device := resourceapi.Device{
			Name: fmt.Sprintf("gpu-%d", i),
			Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
				"index": {
					IntValue: ptr.To(int64(i)),
				},
				"uuid": {
					StringValue: ptr.To(uuid),
				},
				"model": {
					StringValue: ptr.To("LATEST-GPU-MODEL"),
				},
				"driverVersion": {
					VersionValue: ptr.To("1.0.0"),
				},
				"resource.kubernetes.io/pciBusID": {
					StringValue: ptr.To(pciBusID),
				},
				"type": {
					StringValue: ptr.To("gpu"),
				},
			},
			Capacity: map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
				"memory": {
					Value: resource.MustParse("80Gi"),
				},
			},
		}
		devices = append(devices, device)
	}

	// vGPU devices (mediated) — identified by mdevUUID
	for i := 0; i < p.numGPUs; i++ {
		for j := 0; j < vGPUsPerGPU; j++ {
			vfPCIBusID := fmt.Sprintf("0000:01:%02d.%d", i+1, j)
			//idx := i*vGPUsPerGPU + j
			device := resourceapi.Device{
				Name: fmt.Sprintf("gpu-%d-vgpu-%d", i, j),
				Attributes: map[resourceapi.QualifiedName]resourceapi.DeviceAttribute{
					"index": {
						IntValue: ptr.To(int64(i)),
					},
					"uuid": {
						StringValue: ptr.To(uuids[i]),
						//StringValue: ptr.To("d2698c15-d97b-417f-9de6-542028c0579c"),
					},
					"model": {
						StringValue: ptr.To("LATEST-GPU-MODEL"),
					},
					"driverVersion": {
						VersionValue: ptr.To("1.0.0"),
					},
					"mdevUUID": {
						//StringValue: ptr.To(mdevUUIDs[idx]),
						StringValue: ptr.To("d2698c15-d97b-417f-9de6-542028c0579c"),
					},
					"resource.kubernetes.io/pciBusID": {
						StringValue: ptr.To(vfPCIBusID),
					},
					"type": {
						StringValue: ptr.To("vgpu"),
					},
				},
				Capacity: map[resourceapi.QualifiedName]resourceapi.DeviceCapacity{
					"memory": {
						Value: resource.MustParse("40Gi"),
					},
				},
			}
			devices = append(devices, device)
		}
	}

	resources := resourceslice.DriverResources{
		Pools: map[string]resourceslice.Pool{
			p.nodeName: {
				Slices: []resourceslice.Slice{
					{
						Devices: devices,
					},
				},
			},
		},
	}

	return resources, nil
}

func generateUUIDs(seed string, count int) []string {
	rand := rand.New(rand.NewSource(hash(seed)))

	uuids := make([]string, count)
	for i := 0; i < count; i++ {
		charset := make([]byte, 16)
		rand.Read(charset)
		uuid, _ := uuid.FromBytes(charset)
		uuids[i] = "gpu-" + uuid.String()
	}

	return uuids
}

func generateMDevUUIDs(seed string, count int) []string {
	rand := rand.New(rand.NewSource(hash(seed + "-mdev")))

	uuids := make([]string, count)
	for i := 0; i < count; i++ {
		charset := make([]byte, 16)
		rand.Read(charset)
		u, _ := uuid.FromBytes(charset)
		uuids[i] = u.String()
	}

	return uuids
}

func hash(s string) int64 {
	h := int64(0)
	for _, c := range s {
		h = 31*h + int64(c)
	}
	return h
}

// SchemeBuilder implements [profiles.ConfigHandler].
func (p Profile) SchemeBuilder() runtime.SchemeBuilder {
	return runtime.NewSchemeBuilder(
		configapi.AddToScheme,
	)
}

// Validate implements [profiles.ConfigHandler].
func (p Profile) Validate(config runtime.Object) error {
	gpuConfig, ok := config.(*configapi.GpuConfig)
	if !ok {
		return fmt.Errorf("expected v1alpha1.GpuConfig but got: %T", config)
	}
	return gpuConfig.Validate()
}

// ApplyConfig implements [profiles.ConfigHandler].
func (p Profile) ApplyConfig(config runtime.Object, results []*resourceapi.DeviceRequestAllocationResult) (profiles.PerDeviceCDIContainerEdits, error) {
	if config == nil {
		config = configapi.DefaultGpuConfig()
	}
	if config, ok := config.(*configapi.GpuConfig); ok {
		return applyGpuConfig(config, results)
	}
	return nil, fmt.Errorf("runtime object is not a recognized configuration")
}

// In this example driver there is no actual configuration applied. We simply
// define a set of environment variables to be injected into the containers
// that include a given device. A real driver would likely need to do some sort
// of hardware configuration as well, based on the config passed in.
func applyGpuConfig(config *configapi.GpuConfig, results []*resourceapi.DeviceRequestAllocationResult) (profiles.PerDeviceCDIContainerEdits, error) {
	perDeviceEdits := make(profiles.PerDeviceCDIContainerEdits)

	// Normalize the config to set any implied defaults.
	if err := config.Normalize(); err != nil {
		return nil, fmt.Errorf("error normalizing GPU config: %w", err)
	}

	// Validate the config to ensure its integrity.
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("error validating GPU config: %w", err)
	}

	for _, result := range results {
		envs := []string{
			fmt.Sprintf("GPU_DEVICE_%s=%s", result.Device[4:], result.Device),
		}

		if config.Sharing != nil {
			envs = append(envs, fmt.Sprintf("GPU_DEVICE_%s_SHARING_STRATEGY=%s", result.Device[4:], config.Sharing.Strategy))
		}

		switch {
		case config.Sharing.IsTimeSlicing():
			tsconfig, err := config.Sharing.GetTimeSlicingConfig()
			if err != nil {
				return nil, fmt.Errorf("unable to get time slicing config for device %v: %w", result.Device, err)
			}
			envs = append(envs, fmt.Sprintf("GPU_DEVICE_%s_TIMESLICE_INTERVAL=%v", result.Device[4:], tsconfig.Interval))
		case config.Sharing.IsSpacePartitioning():
			spconfig, err := config.Sharing.GetSpacePartitioningConfig()
			if err != nil {
				return nil, fmt.Errorf("unable to get space partitioning config for device %v: %w", result.Device, err)
			}
			envs = append(envs, fmt.Sprintf("GPU_DEVICE_%s_PARTITION_COUNT=%v", result.Device[4:], spconfig.PartitionCount))
		}

		edits := &cdispec.ContainerEdits{
			Env: envs,
		}

		// For vGPU devices (mediated devices), add the VFIO device nodes
		// Check if device name contains "vgpu" to identify mediated devices
		if isVGPUDevice(result.Device) {
			deviceNodes, err := getVFIODeviceNodesForMdev(result.Device)
			if err != nil {
				return nil, fmt.Errorf("unable to get VFIO device nodes for %s: %w", result.Device, err)
			}
			edits.DeviceNodes = deviceNodes
		}

		perDeviceEdits[result.Device] = &cdiapi.ContainerEdits{ContainerEdits: edits}
	}

	return perDeviceEdits, nil
}

// isVGPUDevice checks if a device is a vGPU (mediated device)
func isVGPUDevice(deviceName string) bool {
	// Device names for vGPUs follow the pattern "gpu-X-vgpu-Y"
	// Use strings.Contains to safely check for the vgpu pattern
	return strings.Contains(deviceName, "-vgpu-")
}

// getVFIODeviceNodesForMdev returns the VFIO device nodes needed for a mediated device
func getVFIODeviceNodesForMdev(deviceName string) ([]*cdispec.DeviceNode, error) {
	// For mediated devices, we need:
	// 1. /dev/vfio/vfio - the VFIO container device
	// 2. /dev/vfio/<iommu_group> - the specific IOMMU group device

	// The hardcoded mdev UUID from the driver
	mdevUUID := "d2698c15-d97b-417f-9de6-542028c0579c"

	// Try to find the IOMMU group for this mdev
	iommuGroupPath := filepath.Join("/sys/bus/mdev/devices", mdevUUID, "iommu_group")

	var deviceNodes []*cdispec.DeviceNode

	// Check if the mdev exists and get its IOMMU group
	if target, err := os.Readlink(iommuGroupPath); err == nil {
		// Extract the group number from the symlink target
		// The target looks like: ../../../../kernel/iommu_groups/X
		groupNum := filepath.Base(target)

		// Add both the VFIO container and the specific group device
		deviceNodes = append(deviceNodes,
			&cdispec.DeviceNode{
				Path: "/dev/vfio/vfio",
				Type: "c",
			},
			&cdispec.DeviceNode{
				Path: filepath.Join("/dev/vfio", groupNum),
				Type: "c",
			},
		)
	} else {
		// If we can't determine the IOMMU group (e.g., in test environments
		// without actual mediated devices), don't add any VFIO device nodes.
		// This allows regular containers to work without VFIO.
		// VMs will fail if they actually need the mediated device.
		fmt.Printf("Info: Mediated device %s not found, skipping VFIO device nodes (device: %s)\n", mdevUUID, deviceName)
	}

	return deviceNodes, nil
}
