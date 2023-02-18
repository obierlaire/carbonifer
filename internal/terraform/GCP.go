package terraform

import (
	"fmt"
	"strings"

	"github.com/carboniferio/carbonifer/internal/providers"
	"github.com/carboniferio/carbonifer/internal/resources"
	tfjson "github.com/hashicorp/terraform-json"
	"github.com/shopspring/decimal"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/viper"
)

func GetResource(tfResource tfjson.ConfigResource, dataResources *map[string]resources.DataResource, resourceTemplates *map[string]*tfjson.ConfigResource) resources.Resource {
	resourceId := getResourceIdentification(tfResource)
	if resourceId.ResourceType == "google_compute_instance" {
		specs := getComputeResourceSpecs(tfResource, dataResources, nil)
		return resources.ComputeResource{
			Identification: resourceId,
			Specs:          specs,
		}
	}
	if resourceId.ResourceType == "google_compute_disk" ||
		resourceId.ResourceType == "google_compute_region_disk" {
		specs := getComputeDiskResourceSpecs(tfResource, dataResources)
		return resources.ComputeResource{
			Identification: resourceId,
			Specs:          specs,
		}
	}
	if resourceId.ResourceType == "google_sql_database_instance" {
		specs := getSQLResourceSpecs(tfResource)
		return resources.ComputeResource{
			Identification: resourceId,
			Specs:          specs,
		}
	}
	if resourceId.ResourceType == "google_compute_instance_group_manager" {
		specs, count := getComputeInstanceGroupManagerSpecs(tfResource, dataResources, resourceTemplates)
		if specs != nil {
			resourceId.Count = count
			return resources.ComputeResource{
				Identification: resourceId,
				Specs:          specs,
			}
		}
	}
	return resources.UnsupportedResource{
		Identification: resourceId,
	}
}

func GetResourceTemplate(tfResource tfjson.ConfigResource, dataResources *map[string]resources.DataResource, zone string) resources.Resource {
	resourceId := getResourceIdentification(tfResource)
	if resourceId.ResourceType == "google_compute_instance_template" {
		specs := getComputeResourceSpecs(tfResource, dataResources, zone)
		return resources.ComputeResource{
			Identification: resourceId,
			Specs:          specs,
		}
	}
	return nil
}

func getResourceIdentification(resource tfjson.ConfigResource) *resources.ResourceIdentification {
	region := GetConstFromConfig(&resource, "region")
	if region == nil {
		zone := GetConstFromConfig(&resource, "zone")
		replica_zones := GetConstFromConfig(&resource, "replica_zones")
		if zone != nil {
			region = strings.Join(strings.Split(zone.(string), "-")[:2], "-")
		} else if replica_zones != nil {
			region = strings.Join(strings.Split(replica_zones.([]interface{})[0].(string), "-")[:2], "-")
		} else {
			region = ""
		}
	}
	selfLinkExpr := GetConstFromConfig(&resource, "self_link")
	var selfLink string
	if selfLinkExpr != nil {
		selfLink = GetConstFromConfig(&resource, "self_link").(string)
	}

	return &resources.ResourceIdentification{
		Name:         resource.Name,
		ResourceType: resource.Type,
		Provider:     providers.GCP,
		Region:       fmt.Sprint(region),
		SelfLink:     selfLink,
		Count:        1,
	}
}

func getComputeResourceSpecs(
	resource tfjson.ConfigResource,
	dataResources *map[string]resources.DataResource, groupZone interface{}) *resources.ComputeResourceSpecs {

	machine_type := GetConstFromConfig(&resource, "machine_type").(string)
	var zone string
	if groupZone != nil {
		zone = groupZone.(string)
	} else {
		zone = GetConstFromConfig(&resource, "zone").(string)
	}

	machineType := providers.GetGCPMachineType(machine_type, zone)
	CPUType, ok := GetConstFromConfig(&resource, "cpu_platform").(string)
	if !ok {
		CPUType = ""
	}

	var disks []disk
	bdExpr, ok_bd := resource.Expressions["boot_disk"]
	if ok_bd {
		bootDisks := bdExpr.NestedBlocks
		for _, bootDiskBlock := range bootDisks {
			bootDisk := getBootDisk(resource.Address, bootDiskBlock, dataResources)
			disks = append(disks, bootDisk)
		}
	}

	diskExpr, ok_bd := resource.Expressions["disk"]
	if ok_bd {
		disksBlocks := diskExpr.NestedBlocks
		for _, diskBlock := range disksBlocks {

			bootDisk := getDisk(resource.Address, diskBlock, false, dataResources)
			disks = append(disks, bootDisk)
		}
	}

	sdExpr, ok_sd := resource.Expressions["scratch_disk"]
	if ok_sd {
		scratchDisks := sdExpr.NestedBlocks
		for range scratchDisks {
			// Each scratch disk is 375GB
			//  source: https://cloud.google.com/compute/docs/disks#localssds
			disks = append(disks, disk{isSSD: true, sizeGb: 375})
		}
	}

	hddSize := decimal.Zero
	ssdSize := decimal.Zero
	for _, disk := range disks {
		if disk.isSSD {
			ssdSize = ssdSize.Add(decimal.NewFromFloat(disk.sizeGb))
		} else {
			hddSize = hddSize.Add(decimal.NewFromFloat(disk.sizeGb))
		}
	}

	gpus := machineType.GPUTypes
	gasI := GetConstFromConfig(&resource, "guest_accelerator")
	if gasI != nil {
		guestAccelerators := gasI.([]interface{})
		for _, gaI := range guestAccelerators {
			ga := gaI.(map[string]interface{})
			gpuCount := ga["count"].(float64)
			gpuType := ga["type"].(string)
			for i := float64(0); i < gpuCount; i++ {
				gpus = append(gpus, gpuType)
			}
		}
	}

	return &resources.ComputeResourceSpecs{
		GpuTypes:          gpus,
		VCPUs:             machineType.Vcpus,
		MemoryMb:          machineType.MemoryMb,
		CPUType:           CPUType,
		SsdStorage:        ssdSize,
		HddStorage:        hddSize,
		ReplicationFactor: 1,
	}
}

func getComputeDiskResourceSpecs(
	resource tfjson.ConfigResource,
	dataResources *map[string]resources.DataResource) *resources.ComputeResourceSpecs {

	disk := getDisk(resource.Address, resource.Expressions, false, dataResources)
	hddSize := decimal.Zero
	ssdSize := decimal.Zero
	if disk.isSSD {
		ssdSize = ssdSize.Add(decimal.NewFromFloat(disk.sizeGb))
	} else {
		hddSize = hddSize.Add(decimal.NewFromFloat(disk.sizeGb))
	}
	return &resources.ComputeResourceSpecs{
		SsdStorage:        ssdSize,
		HddStorage:        hddSize,
		ReplicationFactor: disk.replicationFactor,
	}
}

type disk struct {
	sizeGb            float64
	isSSD             bool
	replicationFactor int32
}

func getBootDisk(resourceAddress string, bootDiskBlock map[string]*tfjson.Expression, dataResources *map[string]resources.DataResource) disk {
	var disk disk
	initParams := bootDiskBlock["initialize_params"].NestedBlocks
	for _, initParam := range initParams {
		disk = getDisk(resourceAddress, initParam, true, dataResources)

	}
	return disk
}

func getDisk(resourceAddress string, diskBlock map[string]*tfjson.Expression, isBootDiskParam bool, dataResources *map[string]resources.DataResource) disk {
	disk := disk{
		sizeGb:            viper.GetFloat64("provider.gcp.boot_disk.size"),
		isSSD:             true,
		replicationFactor: 1,
	}

	// Is Boot disk
	isBootDisk := isBootDiskParam
	isBootDiskI := GetConstFromExpression(diskBlock["boot"])
	if isBootDiskI != nil {
		isBootDisk = isBootDiskI.(bool)
	}

	// Get disk type
	var diskType string
	diskTypeExpr := diskBlock["type"]
	if diskTypeExpr == nil {
		if isBootDisk {
			diskType = viper.GetString("provider.gcp.boot_disk.type")
		} else {
			diskType = viper.GetString("provider.gcp.disk.type")
		}
	} else {
		diskType = diskTypeExpr.ConstantValue.(string)
	}

	if diskType == "pd-standard" {
		disk.isSSD = false
	}

	// Get Disk size
	declaredSize := GetConstFromExpression(diskBlock["size"])
	if declaredSize == nil {
		declaredSize = GetConstFromExpression(diskBlock["disk_size_gb"])
	}
	if declaredSize == nil {
		if isBootDisk {
			disk.sizeGb = viper.GetFloat64("provider.gcp.boot_disk.size")
		} else {
			disk.sizeGb = viper.GetFloat64("provider.gcp.disk.size")
		}
		diskImageLinkExpr, okImage := diskBlock["image"]
		if okImage {
			for _, ref := range diskImageLinkExpr.References {
				image, ok := (*dataResources)[ref]
				if ok {
					disk.sizeGb = (image.(resources.DataImageResource)).DataImageSpecs.DiskSizeGb
				} else {
					log.Warningf("%v : Disk image does not have a size declared, considering it default to be 10Gb ", resourceAddress)
				}
			}
		} else {
			log.Warningf("%v : Boot disk size not declared. Please set it! (otherwise we assume 10gb) ", resourceAddress)

		}
	} else {
		disk.sizeGb = declaredSize.(float64)
	}

	replicaZonesExpr := diskBlock["replica_zones"]
	if replicaZonesExpr != nil {
		rz := replicaZonesExpr.ConstantValue.([]interface{})
		disk.replicationFactor = int32(len(rz))
	} else {
		disk.replicationFactor = 1
	}

	return disk
}

func getSQLResourceSpecs(
	resource tfjson.ConfigResource) *resources.ComputeResourceSpecs {

	replicationFactor := int32(1)
	ssdSize := decimal.Zero
	hddSize := decimal.Zero
	var tier providers.SqlTier

	settingsExpr, ok := resource.Expressions["settings"]
	if ok {
		settings := settingsExpr.NestedBlocks[0]

		availabilityType := settings["availability_type"]
		if availabilityType.ConstantValue != nil && availabilityType.ConstantValue == "REGIONAL" {
			replicationFactor = int32(2)
		}

		tierName := ""
		if settings["tier"] != nil {
			tierName = settings["tier"].ConstantValue.(string)
		}
		tier = providers.GetGCPSQLTier(tierName)

		diskTypeI, ok_dt := settings["disk_type"]
		diskType := "PD_SSD"
		if ok_dt {
			diskType = diskTypeI.ConstantValue.(string)
		}

		diskSizeI, ok_ds := settings["disk_size"]
		diskSize := decimal.NewFromFloat(10)
		if ok_ds {
			diskSize = decimal.NewFromFloat(diskSizeI.ConstantValue.(float64))
		}

		if diskType == "PD_SSD" {
			ssdSize = diskSize
		} else if diskType == "PD_HDD" {
			hddSize = diskSize
		} else {
			log.Fatalf("%s : wrong type of disk : %s", resource.Address, tierName)
		}

	}

	return &resources.ComputeResourceSpecs{
		VCPUs:             int32(tier.Vcpus),
		MemoryMb:          int32(tier.MemoryMb),
		SsdStorage:        ssdSize,
		HddStorage:        hddSize,
		ReplicationFactor: replicationFactor,
	}
}

func getComputeInstanceGroupManagerSpecs(tfResource tfjson.ConfigResource, dataResources *map[string]resources.DataResource, resourceTemplates *map[string]*tfjson.ConfigResource) (*resources.ComputeResourceSpecs, int64) {
	targetSize := int64(0)
	targetSizeExpr := GetConstFromConfig(&tfResource, "target_size")
	if targetSizeExpr != nil {
		targetSize = decimal.NewFromFloat(targetSizeExpr.(float64)).BigInt().Int64()
	}
	versionExpr := tfResource.Expressions["version"]
	var template *tfjson.ConfigResource
	if versionExpr != nil {
		for _, version := range versionExpr.NestedBlocks {
			instanceTemplate := version["instance_template"]
			if instanceTemplate != nil {
				references := instanceTemplate.References
				for _, reference := range references {
					if !strings.HasSuffix(reference, ".id") {
						template = (*resourceTemplates)[reference]
					}
				}
			}
		}
	}
	if template != nil {
		zone := GetConstFromConfig(&tfResource, "zone").(string)
		templateResource := GetResourceTemplate(*template, dataResources, zone)
		computeTemplate, ok := templateResource.(resources.ComputeResource)
		if ok {
			return computeTemplate.Specs, targetSize
		} else {
			log.Fatalf("Type mismatch, not a esources.ComputeResource template %v", computeTemplate.GetAddress())
		}
	}
	return nil, 0
}

func GetConstFromConfig(resource *tfjson.ConfigResource, key string) interface{} {
	expr := resource.Expressions[key]
	return GetConstFromExpression(expr)
}

func GetConstFromExpression(expr *tfjson.Expression) interface{} {
	if expr != nil {
		if expr.ConstantValue != nil {
			return expr.ConstantValue
		}
	}
	return nil
}
