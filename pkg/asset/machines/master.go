package machines

import (
	"fmt"

	machineapi "github.com/openshift/cluster-api/pkg/apis/machine/v1beta1"
	"github.com/pkg/errors"
	clusterapi "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"

	"github.com/openshift/installer/pkg/asset"
	"github.com/openshift/installer/pkg/asset/ignition/machine"
	"github.com/openshift/installer/pkg/asset/installconfig"
	"github.com/openshift/installer/pkg/asset/machines/aws"
	"github.com/openshift/installer/pkg/asset/machines/libvirt"
	"github.com/openshift/installer/pkg/asset/machines/openstack"
	"github.com/openshift/installer/pkg/asset/rhcos"
	"github.com/openshift/installer/pkg/types"
	awstypes "github.com/openshift/installer/pkg/types/aws"
	libvirttypes "github.com/openshift/installer/pkg/types/libvirt"
	nonetypes "github.com/openshift/installer/pkg/types/none"
	openstacktypes "github.com/openshift/installer/pkg/types/openstack"
)

// Master generates the machines for the `master` machine pool.
type Master struct {
	Machines           []machineapi.Machine
	MachinesDeprecated []clusterapi.Machine
	UserDataSecretRaw  []byte
}

var _ asset.Asset = (*Master)(nil)

// Name returns a human friendly name for the Master Asset.
func (m *Master) Name() string {
	return "Master Machines"
}

// Dependencies returns all of the dependencies directly needed by the
// Master asset
func (m *Master) Dependencies() []asset.Asset {
	return []asset.Asset{
		&installconfig.ClusterID{},
		// PlatformCredsCheck just checks the creds (and asks, if needed)
		// We do not actually use it in this asset directly, hence
		// it is put in the dependencies but not fetched in Generate
		&installconfig.PlatformCredsCheck{},
		&installconfig.InstallConfig{},
		new(rhcos.Image),
		&machine.Master{},
	}
}

// Generate generates the Master asset.
func (m *Master) Generate(dependencies asset.Parents) error {
	clusterID := &installconfig.ClusterID{}
	installconfig := &installconfig.InstallConfig{}
	rhcosImage := new(rhcos.Image)
	mign := &machine.Master{}
	dependencies.Get(clusterID, installconfig, rhcosImage, mign)

	var err error
	userDataMap := map[string][]byte{"master-user-data": mign.File.Data}
	m.UserDataSecretRaw, err = userDataList(userDataMap)
	if err != nil {
		return errors.Wrap(err, "failed to create user-data secret for master machines")
	}

	ic := installconfig.Config
	pool := masterPool(ic.Machines)
	switch ic.Platform.Name() {
	case awstypes.Name:
		mpool := defaultAWSMachinePoolPlatform()
		mpool.InstanceType = "m4.xlarge"
		mpool.Set(ic.Platform.AWS.DefaultMachinePlatform)
		mpool.Set(pool.Platform.AWS)
		if len(mpool.Zones) == 0 {
			azs, err := aws.AvailabilityZones(ic.Platform.AWS.Region)
			if err != nil {
				return errors.Wrap(err, "failed to fetch availability zones")
			}
			mpool.Zones = azs
		}
		pool.Platform.AWS = &mpool
		m.Machines, err = aws.Machines(clusterID.ClusterID, ic, &pool, string(*rhcosImage), "master", "master-user-data")
		if err != nil {
			return errors.Wrap(err, "failed to create master machine objects")
		}
		aws.ConfigMasters(m.Machines, ic.ObjectMeta.Name)
	case libvirttypes.Name:
		mpool := defaultLibvirtMachinePoolPlatform()
		mpool.Set(ic.Platform.Libvirt.DefaultMachinePlatform)
		mpool.Set(pool.Platform.Libvirt)
		pool.Platform.Libvirt = &mpool
		m.Machines, err = libvirt.Machines(clusterID.ClusterID, ic, &pool, "master", "master-user-data")
		if err != nil {
			return errors.Wrap(err, "failed to create master machine objects")
		}
	case nonetypes.Name:
		// This is needed to ensure that roundtrip generate-load tests pass when
		// comparing this value. Otherwise, generate will use a nil value while
		// load will use an empty slice.
		m.Machines = []machineapi.Machine{}
	case openstacktypes.Name:
		mpool := defaultOpenStackMachinePoolPlatform(ic.Platform.OpenStack.FlavorName)
		mpool.Set(ic.Platform.OpenStack.DefaultMachinePlatform)
		mpool.Set(pool.Platform.OpenStack)
		pool.Platform.OpenStack = &mpool

		m.MachinesDeprecated, err = openstack.Machines(clusterID.ClusterID, ic, &pool, string(*rhcosImage), "master", "master-user-data")
		if err != nil {
			return errors.Wrap(err, "failed to create master machine objects")
		}
		openstack.ConfigMasters(m.MachinesDeprecated, ic.ObjectMeta.Name)
	default:
		return fmt.Errorf("invalid Platform")
	}
	return nil
}

func masterPool(pools []types.MachinePool) types.MachinePool {
	for idx, pool := range pools {
		if pool.Name == "master" {
			return pools[idx]
		}
	}
	return types.MachinePool{}
}
