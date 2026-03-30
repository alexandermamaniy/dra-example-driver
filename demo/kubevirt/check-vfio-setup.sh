#!/usr/bin/env bash

# Copyright The Kubernetes Authors.
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

# This script checks if VFIO is properly configured for mediated devices

set -e

MDEV_UUID="d2698c15-d97b-417f-9de6-542028c0579c"

echo "=== Checking VFIO Setup for Mediated Devices ==="
echo

# Check if VFIO modules are loaded
echo "1. Checking VFIO kernel modules..."
if lsmod | grep -q vfio; then
    echo "✓ VFIO modules are loaded:"
    lsmod | grep vfio
else
    echo "✗ VFIO modules are NOT loaded"
    echo "  Load them with: sudo modprobe vfio vfio_mdev vfio_iommu_type1"
    exit 1
fi
echo

# Check if the mediated device exists
echo "2. Checking if mediated device exists..."
if [ -d "/sys/bus/mdev/devices/${MDEV_UUID}" ]; then
    echo "✓ Mediated device ${MDEV_UUID} exists"
else
    echo "✗ Mediated device ${MDEV_UUID} does NOT exist"
    echo "  Create it following the instructions in demo/kubevirt/mtty/README.md"
    exit 1
fi
echo

# Check IOMMU group
echo "3. Checking IOMMU group..."
if [ -L "/sys/bus/mdev/devices/${MDEV_UUID}/iommu_group" ]; then
    IOMMU_GROUP=$(basename $(readlink "/sys/bus/mdev/devices/${MDEV_UUID}/iommu_group"))
    echo "✓ IOMMU group: ${IOMMU_GROUP}"
    
    # Check if the VFIO device node exists
    if [ -c "/dev/vfio/${IOMMU_GROUP}" ]; then
        echo "✓ VFIO device node exists: /dev/vfio/${IOMMU_GROUP}"
        ls -l "/dev/vfio/${IOMMU_GROUP}"
    else
        echo "✗ VFIO device node does NOT exist: /dev/vfio/${IOMMU_GROUP}"
        exit 1
    fi
else
    echo "✗ Cannot determine IOMMU group"
    exit 1
fi
echo

# Check VFIO container device
echo "4. Checking VFIO container device..."
if [ -c "/dev/vfio/vfio" ]; then
    echo "✓ VFIO container device exists: /dev/vfio/vfio"
    ls -l /dev/vfio/vfio
else
    echo "✗ VFIO container device does NOT exist: /dev/vfio/vfio"
    exit 1
fi
echo

echo "=== All VFIO checks passed! ==="
echo
echo "The mediated device is properly configured and should work with KubeVirt."
echo "IOMMU Group: ${IOMMU_GROUP}"
echo "Required device nodes:"
echo "  - /dev/vfio/vfio"
echo "  - /dev/vfio/${IOMMU_GROUP}"
