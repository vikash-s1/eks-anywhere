package framework

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sigs.k8s.io/cluster-api/bootstrap/kubeadm/api/v1beta1"

	"github.com/aws/eks-anywhere/internal/pkg/api"
	"github.com/aws/eks-anywhere/internal/test/cleanup"
	anywherev1 "github.com/aws/eks-anywhere/pkg/api/v1alpha1"
	"github.com/aws/eks-anywhere/pkg/executables"
	"github.com/aws/eks-anywhere/pkg/manifests/bundles"
	"github.com/aws/eks-anywhere/pkg/manifests/releases"
	anywheretypes "github.com/aws/eks-anywhere/pkg/types"
	releasev1 "github.com/aws/eks-anywhere/release/api/v1alpha1"
	clusterf "github.com/aws/eks-anywhere/test/framework/cluster"
)

const (
	vsphereDatacenterVar        = "T_VSPHERE_DATACENTER"
	vsphereDatastoreVar         = "T_VSPHERE_DATASTORE"
	vsphereFolderVar            = "T_VSPHERE_FOLDER"
	vsphereNetworkVar           = "T_VSPHERE_NETWORK"
	vspherePrivateNetworkVar    = "T_VSPHERE_PRIVATE_NETWORK"
	vsphereResourcePoolVar      = "T_VSPHERE_RESOURCE_POOL"
	vsphereServerVar            = "T_VSPHERE_SERVER"
	vsphereSshAuthorizedKeyVar  = "T_VSPHERE_SSH_AUTHORIZED_KEY"
	vsphereStoragePolicyNameVar = "T_VSPHERE_STORAGE_POLICY_NAME"
	vsphereTlsInsecureVar       = "T_VSPHERE_TLS_INSECURE"
	vsphereTlsThumbprintVar     = "T_VSPHERE_TLS_THUMBPRINT"
	vsphereUsernameVar          = "EKSA_VSPHERE_USERNAME"
	vspherePasswordVar          = "EKSA_VSPHERE_PASSWORD"
	cidrVar                     = "T_VSPHERE_CIDR"
	privateNetworkCidrVar       = "T_VSPHERE_PRIVATE_NETWORK_CIDR"
	govcUrlVar                  = "VSPHERE_SERVER"
	govcInsecureVar             = "GOVC_INSECURE"
	govcDatacenterVar           = "GOVC_DATACENTER"
	vsphereTemplateEnvVarPrefix = "T_VSPHERE_TEMPLATE_"
	vsphereTemplatesFolder      = "T_VSPHERE_TEMPLATE_FOLDER"
	vsphereTestTagEnvVar        = "T_VSPHERE_TAG"
)

var requiredEnvVars = []string{
	vsphereDatacenterVar,
	vsphereDatastoreVar,
	vsphereFolderVar,
	vsphereNetworkVar,
	vspherePrivateNetworkVar,
	vsphereResourcePoolVar,
	vsphereServerVar,
	vsphereSshAuthorizedKeyVar,
	vsphereTlsInsecureVar,
	vsphereTlsThumbprintVar,
	vsphereUsernameVar,
	vspherePasswordVar,
	cidrVar,
	privateNetworkCidrVar,
	govcUrlVar,
	govcInsecureVar,
	govcDatacenterVar,
	vsphereTestTagEnvVar,
}

type VSphere struct {
	t                 *testing.T
	testsConfig       vsphereConfig
	fillers           []api.VSphereFiller
	clusterFillers    []api.ClusterFiller
	cidr              string
	GovcClient        *executables.Govc
	devRelease        *releasev1.EksARelease
	templatesRegistry *templateRegistry
}

type vsphereConfig struct {
	Datacenter        string
	Datastore         string
	Folder            string
	Network           string
	ResourcePool      string
	Server            string
	SSHAuthorizedKey  string
	StoragePolicyName string
	TLSInsecure       bool
	TLSThumbprint     string
	TemplatesFolder   string
}

// VSphereOpt is construction option for the E2E vSphere provider.
type VSphereOpt func(*VSphere)

func NewVSphere(t *testing.T, opts ...VSphereOpt) *VSphere {
	checkRequiredEnvVars(t, requiredEnvVars)
	c := buildGovc(t)
	config, err := readVSphereConfig()
	if err != nil {
		t.Fatalf("Failed reading vSphere tests config: %v", err)
	}
	v := &VSphere{
		t:           t,
		GovcClient:  c,
		testsConfig: config,
		fillers: []api.VSphereFiller{
			api.WithDatacenter(config.Datacenter),
			api.WithDatastoreForAllMachines(config.Datastore),
			api.WithFolderForAllMachines(config.Folder),
			api.WithNetwork(config.Network),
			api.WithResourcePoolForAllMachines(config.ResourcePool),
			api.WithServer(config.Server),
			api.WithSSHAuthorizedKeyForAllMachines(config.SSHAuthorizedKey),
			api.WithStoragePolicyNameForAllMachines(config.StoragePolicyName),
			api.WithTLSInsecure(config.TLSInsecure),
			api.WithTLSThumbprint(config.TLSThumbprint),
		},
	}

	v.cidr = os.Getenv(cidrVar)
	v.templatesRegistry = &templateRegistry{cache: map[string]string{}, generator: v}
	for _, opt := range opts {
		opt(v)
	}

	return v
}

// withVSphereKubeVersionAndOS returns a VSphereOpt that adds API fillers to use a vSphere template for
// the specified OS family and version (default if not provided), corresponding to a particular
// Kubernetes version, in addition to configuring all machine configs to use this OS family.
func withVSphereKubeVersionAndOS(kubeVersion anywherev1.KubernetesVersion, os OS, release *releasev1.EksARelease) VSphereOpt {
	return func(v *VSphere) {
		v.fillers = append(v.fillers,
			v.templateForKubeVersionAndOS(kubeVersion, os, release),
			api.WithOsFamilyForAllMachines(osFamiliesForOS[os]),
		)
	}
}

// WithRedHat128VSphere vsphere test with Redhat 8 for Kubernetes 1.28.
func WithRedHat128VSphere() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube128, RedHat8, nil)
}

// WithRedHat129VSphere vsphere test with Redhat 8 for Kubernetes 1.29.
func WithRedHat129VSphere() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube129, RedHat8, nil)
}

// WithRedHat130VSphere vsphere test with Redhat 8 for Kubernetes 1.30.
func WithRedHat130VSphere() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube130, RedHat8, nil)
}

// WithRedHat131VSphere vsphere test with Redhat 8 for Kubernetes 1.31.
func WithRedHat131VSphere() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube131, RedHat8, nil)
}

// WithRedHat9128VSphere vsphere test with Redhat 9 for Kubernetes 1.28.
func WithRedHat9128VSphere() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube128, RedHat9, nil)
}

// WithRedHat9129VSphere vsphere test with Redhat 9 for Kubernetes 1.29.
func WithRedHat9129VSphere() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube129, RedHat9, nil)
}

// WithRedHat9130VSphere vsphere test with Redhat 9 for Kubernetes 1.30.
func WithRedHat9130VSphere() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube130, RedHat9, nil)
}

// WithRedHat9131VSphere vsphere test with Redhat 9 for Kubernetes 1.31.
func WithRedHat9131VSphere() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube131, RedHat9, nil)
}

// WithRedHat9132VSphere vsphere test with Redhat 9 for Kubernetes 1.32.
func WithRedHat9132VSphere() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube132, RedHat9, nil)
}

// WithRedHat9133VSphere vsphere test with Redhat 9 for Kubernetes 1.33.
func WithRedHat9133VSphere() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube133, RedHat9, nil)
}

// WithUbuntu128 returns a VSphereOpt that adds API fillers to use a Ubuntu vSphere template for k8s 1.28
// and the "ubuntu" osFamily in all machine configs.
func WithUbuntu128() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube128, Ubuntu2004, nil)
}

// WithUbuntu129 returns a VSphereOpt that adds API fillers to use a Ubuntu vSphere template for k8s 1.29
// and the "ubuntu" osFamily in all machine configs.
func WithUbuntu129() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube129, Ubuntu2004, nil)
}

// WithUbuntu130 returns a VSphereOpt that adds API fillers to use a Ubuntu vSphere template for k8s 1.30
// and the "ubuntu" osFamily in all machine configs.
func WithUbuntu130() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube130, Ubuntu2004, nil)
}

// WithUbuntu131 returns a VSphereOpt that adds API fillers to use a Ubuntu vSphere template for k8s 1.31
// and the "ubuntu" osFamily in all machine configs.
func WithUbuntu131() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube131, Ubuntu2004, nil)
}

// WithUbuntu132 returns a VSphereOpt that adds API fillers to use a Ubuntu vSphere template for k8s 1.32
// and the "ubuntu" osFamily in all machine configs.
func WithUbuntu132() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube132, Ubuntu2004, nil)
}

// WithUbuntu133 returns a VSphereOpt that adds API fillers to use a Ubuntu vSphere template for k8s 1.33
// and the "ubuntu" osFamily in all machine configs.
func WithUbuntu133() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube133, Ubuntu2004, nil)
}

// WithBottleRocket128 returns br 1.28 var.
func WithBottleRocket128() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube128, Bottlerocket1, nil)
}

// WithBottleRocket129 returns br 1.29 var.
func WithBottleRocket129() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube129, Bottlerocket1, nil)
}

// WithBottleRocket130 returns br 1.30 var.
func WithBottleRocket130() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube130, Bottlerocket1, nil)
}

// WithBottleRocket131 returns br 1.31 var.
func WithBottleRocket131() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube131, Bottlerocket1, nil)
}

// WithBottleRocket132 returns br 1.32 var.
func WithBottleRocket132() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube132, Bottlerocket1, nil)
}

// WithBottleRocket133 returns br 1.33 var.
func WithBottleRocket133() VSphereOpt {
	return withVSphereKubeVersionAndOS(anywherev1.Kube133, Bottlerocket1, nil)
}

func WithPrivateNetwork() VSphereOpt {
	return func(v *VSphere) {
		v.fillers = append(v.fillers,
			api.WithVSphereStringFromEnvVar(vspherePrivateNetworkVar, api.WithNetwork),
		)
		v.cidr = os.Getenv(privateNetworkCidrVar)
	}
}

// WithLinkedCloneMode sets clone mode to LinkedClone for all the machine.
func WithLinkedCloneMode() VSphereOpt {
	return func(v *VSphere) {
		v.fillers = append(v.fillers,
			api.WithCloneModeForAllMachines(anywherev1.LinkedClone),
		)
	}
}

// WithFullCloneMode sets clone mode to FullClone for all the machine.
func WithFullCloneMode() VSphereOpt {
	return func(v *VSphere) {
		v.fillers = append(v.fillers,
			api.WithCloneModeForAllMachines(anywherev1.FullClone),
		)
	}
}

// WithDiskGiBForAllMachines sets diskGiB for all the machines.
func WithDiskGiBForAllMachines(value int) VSphereOpt {
	return func(v *VSphere) {
		v.fillers = append(v.fillers,
			api.WithDiskGiBForAllMachines(value),
		)
	}
}

// WithNTPServersForAllMachines sets NTP servers for all the machines.
func WithNTPServersForAllMachines() VSphereOpt {
	return func(v *VSphere) {
		checkRequiredEnvVars(v.t, RequiredNTPServersEnvVars())
		v.fillers = append(v.fillers,
			api.WithNTPServersForAllMachines(GetNTPServersFromEnv()),
		)
	}
}

// WithBottlerocketKubernetesSettingsForAllMachines sets Bottlerocket Kubernetes settings for all the machines.
func WithBottlerocketKubernetesSettingsForAllMachines() VSphereOpt {
	return func(v *VSphere) {
		checkRequiredEnvVars(v.t, RequiredBottlerocketKubernetesSettingsEnvVars())
		unsafeSysctls, clusterDNSIPS, maxPods, err := GetBottlerocketKubernetesSettingsFromEnv()
		if err != nil {
			v.t.Fatalf("failed to get bottlerocket kubernetes settings from env: %v", err)
		}
		config := &anywherev1.BottlerocketConfiguration{
			Kubernetes: &v1beta1.BottlerocketKubernetesSettings{
				AllowedUnsafeSysctls: unsafeSysctls,
				ClusterDNSIPs:        clusterDNSIPS,
				MaxPods:              maxPods,
			},
		}
		v.fillers = append(v.fillers,
			api.WithBottlerocketConfigurationForAllMachines(config),
		)
	}
}

// WithSSHAuthorizedKeyForAllMachines sets SSH authorized keys for all the machines.
func WithSSHAuthorizedKeyForAllMachines(sshKey string) VSphereOpt {
	return func(v *VSphere) {
		v.fillers = append(v.fillers, api.WithSSHAuthorizedKeyForAllMachines(sshKey))
	}
}

// WithVSphereTags with vsphere tags option.
func WithVSphereTags() VSphereOpt {
	return func(v *VSphere) {
		tags := []string{os.Getenv(vsphereTestTagEnvVar)}
		v.fillers = append(v.fillers,
			api.WithTagsForAllMachines(tags),
		)
	}
}

func WithVSphereWorkerNodeGroup(name string, workerNodeGroup *WorkerNodeGroup, fillers ...api.VSphereMachineConfigFiller) VSphereOpt {
	return func(v *VSphere) {
		v.fillers = append(v.fillers, vSphereMachineConfig(name, fillers...))

		v.clusterFillers = append(v.clusterFillers, buildVSphereWorkerNodeGroupClusterFiller(name, workerNodeGroup))
	}
}

// WithMachineTemplate returns an api.ClusterConfigFiller that changes template in machine template.
func (v *VSphere) WithMachineTemplate(machineName, template string) api.ClusterConfigFiller {
	return api.JoinClusterConfigFillers(
		api.VSphereToConfigFiller(api.WithMachineTemplate(machineName, template)),
	)
}

// WithNewWorkerNodeGroup returns an api.ClusterFiller that adds a new workerNodeGroupConfiguration and
// a corresponding VSphereMachineConfig to the cluster config.
func (v *VSphere) WithNewWorkerNodeGroup(name string, workerNodeGroup *WorkerNodeGroup) api.ClusterConfigFiller {
	machineConfigFillers := []api.VSphereMachineConfigFiller{updateMachineSSHAuthorizedKey()}
	return api.JoinClusterConfigFillers(
		api.VSphereToConfigFiller(vSphereMachineConfig(name, machineConfigFillers...)),
		api.ClusterToConfigFiller(buildVSphereWorkerNodeGroupClusterFiller(name, workerNodeGroup)),
	)
}

// WithWorkerNodeGroupConfiguration returns an api.ClusterFiller that adds a new workerNodeGroupConfiguration item to the cluster config.
func (v *VSphere) WithWorkerNodeGroupConfiguration(name string, workerNodeGroup *WorkerNodeGroup) api.ClusterConfigFiller {
	return api.ClusterToConfigFiller(buildVSphereWorkerNodeGroupClusterFiller(name, workerNodeGroup))
}

// updateMachineSSHAuthorizedKey updates a vsphere machine configs SSHAuthorizedKey.
func updateMachineSSHAuthorizedKey() api.VSphereMachineConfigFiller {
	return api.WithStringFromEnvVar(vsphereSshAuthorizedKeyVar, api.WithSSHKey)
}

// WithVSphereFillers adds VSphereFiller to the provider default fillers.
func WithVSphereFillers(fillers ...api.VSphereFiller) VSphereOpt {
	return func(v *VSphere) {
		v.fillers = append(v.fillers, fillers...)
	}
}

// Name returns the provider name. It satisfies the test framework Provider.
func (v *VSphere) Name() string {
	return "vsphere"
}

// Setup does nothing. It satisfies the test framework Provider.
func (v *VSphere) Setup() {}

// UpdateKubeConfig customizes generated kubeconfig for the provider.
func (v *VSphere) UpdateKubeConfig(content *[]byte, clusterName string) error {
	return nil
}

// ClusterConfigUpdates satisfies the test framework Provider.
func (v *VSphere) ClusterConfigUpdates() []api.ClusterConfigFiller {
	clusterIP, err := GetIP(v.cidr, ClusterIPPoolEnvVar)
	if err != nil {
		v.t.Fatalf("failed to get cluster ip for test environment: %v", err)
	}

	f := make([]api.ClusterFiller, 0, len(v.clusterFillers)+1)
	f = append(f, v.clusterFillers...)
	f = append(f, api.WithControlPlaneEndpointIP(clusterIP))

	return []api.ClusterConfigFiller{api.ClusterToConfigFiller(f...), api.VSphereToConfigFiller(v.fillers...)}
}

// WithKubeVersionAndOS returns a cluster config filler that sets the cluster kube version and the right template for all
// vsphere machine configs.
func (v *VSphere) WithKubeVersionAndOS(kubeVersion anywherev1.KubernetesVersion, os OS, release *releasev1.EksARelease, _ ...string) api.ClusterConfigFiller {
	return api.JoinClusterConfigFillers(
		api.ClusterToConfigFiller(api.WithKubernetesVersion(kubeVersion)),
		api.VSphereToConfigFiller(
			v.templateForKubeVersionAndOS(kubeVersion, os, release),
			api.WithOsFamilyForAllMachines(osFamiliesForOS[os]),
		),
	)
}

// WithKubeVersionAndOSMachineConfig returns a cluster config filler that sets the cluster kube version and the right template for a specific
// vsphere machine config.
func (v *VSphere) WithKubeVersionAndOSMachineConfig(name string, kubeVersion anywherev1.KubernetesVersion, os OS) api.ClusterConfigFiller {
	return api.JoinClusterConfigFillers(
		api.VSphereToConfigFiller(
			v.templateForKubeVersionAndOSMachineConfig(name, kubeVersion, os),
		),
	)
}

// WithUbuntu128 returns a cluster config filler that sets the kubernetes version of the cluster to 1.28
// as well as the right ubuntu template and osFamily for all VSphereMachineConfigs.
func (v *VSphere) WithUbuntu128() api.ClusterConfigFiller {
	return v.WithKubeVersionAndOS(anywherev1.Kube128, Ubuntu2004, nil)
}

// WithUbuntu129 returns a cluster config filler that sets the kubernetes version of the cluster to 1.29
// as well as the right ubuntu template and osFamily for all VSphereMachineConfigs.
func (v *VSphere) WithUbuntu129() api.ClusterConfigFiller {
	return v.WithKubeVersionAndOS(anywherev1.Kube129, Ubuntu2004, nil)
}

// WithUbuntu130 returns a cluster config filler that sets the kubernetes version of the cluster to 1.30
// as well as the right ubuntu template and osFamily for all VSphereMachineConfigs.
func (v *VSphere) WithUbuntu130() api.ClusterConfigFiller {
	return v.WithKubeVersionAndOS(anywherev1.Kube130, Ubuntu2004, nil)
}

// WithUbuntu131 returns a cluster config filler that sets the kubernetes version of the cluster to 1.31
// as well as the right ubuntu template and osFamily for all VSphereMachineConfigs.
func (v *VSphere) WithUbuntu131() api.ClusterConfigFiller {
	return v.WithKubeVersionAndOS(anywherev1.Kube131, Ubuntu2004, nil)
}

// WithUbuntu132 returns a cluster config filler that sets the kubernetes version of the cluster to 1.32
// as well as the right ubuntu template and osFamily for all VSphereMachineConfigs.
func (v *VSphere) WithUbuntu132() api.ClusterConfigFiller {
	return v.WithKubeVersionAndOS(anywherev1.Kube132, Ubuntu2004, nil)
}

// WithUbuntu133 returns a cluster config filler that sets the kubernetes version of the cluster to 1.33
// as well as the right ubuntu template and osFamily for all VSphereMachineConfigs.
func (v *VSphere) WithUbuntu133() api.ClusterConfigFiller {
	return v.WithKubeVersionAndOS(anywherev1.Kube133, Ubuntu2004, nil)
}

// WithBottleRocket131 returns a cluster config filler that sets the kubernetes version of the cluster to 1.31
// as well as the right bottlerocket template and osFamily for all VSphereMachineConfigs.
func (v *VSphere) WithBottleRocket131() api.ClusterConfigFiller {
	return v.WithKubeVersionAndOS(anywherev1.Kube131, Bottlerocket1, nil)
}

// WithBottleRocket132 returns a cluster config filler that sets the kubernetes version of the cluster to 1.32
// as well as the right bottlerocket template and osFamily for all VSphereMachineConfigs.
func (v *VSphere) WithBottleRocket132() api.ClusterConfigFiller {
	return v.WithKubeVersionAndOS(anywherev1.Kube132, Bottlerocket1, nil)
}

// WithBottleRocket133 returns a cluster config filler that sets the kubernetes version of the cluster to 1.33
// as well as the right botlterocket template and osFamily for all VSphereMachineConfigs.
func (v *VSphere) WithBottleRocket133() api.ClusterConfigFiller {
	return v.WithKubeVersionAndOS(anywherev1.Kube133, Bottlerocket1, nil)
}

// CleanupResources deletes all the VMs owned by the test EKS-A cluster. It satisfies the test framework Provider.
func (v *VSphere) CleanupResources(clusterName string) error {
	return cleanup.CleanUpVsphereTestResources(context.Background(), clusterName)
}

func (v *VSphere) WithProviderUpgrade(fillers ...api.VSphereFiller) ClusterE2ETestOpt {
	return func(e *ClusterE2ETest) {
		e.UpdateClusterConfig(api.VSphereToConfigFiller(fillers...))
	}
}

func (v *VSphere) WithProviderUpgradeGit(fillers ...api.VSphereFiller) ClusterE2ETestOpt {
	return func(e *ClusterE2ETest) {
		e.UpdateClusterConfig(api.VSphereToConfigFiller(fillers...))
	}
}

// WithNewVSphereWorkerNodeGroup adds a new worker node group to the cluster config.
func (v *VSphere) WithNewVSphereWorkerNodeGroup(name string, workerNodeGroup *WorkerNodeGroup) ClusterE2ETestOpt {
	return func(e *ClusterE2ETest) {
		e.UpdateClusterConfig(
			api.ClusterToConfigFiller(buildVSphereWorkerNodeGroupClusterFiller(name, workerNodeGroup)),
		)
	}
}

// templateForKubeVersionAndOS returns a vSphere filler for the given OS and Kubernetes version.
func (v *VSphere) templateForKubeVersionAndOS(kubeVersion anywherev1.KubernetesVersion, os OS, release *releasev1.EksARelease) api.VSphereFiller {
	var template string
	useBundlesOverride := getBundlesOverride() == "true"
	if release == nil {
		template = v.templateForDevRelease(kubeVersion, os, useBundlesOverride)
	} else {
		template = v.templatesRegistry.templateForRelease(v.t, release, kubeVersion, os, useBundlesOverride)
	}
	return api.WithTemplateForAllMachines(template)
}

// templateForKubeVersionAndOSMachineConfig returns a vSphere filler for the given OS and Kubernetes version for a specific machine config.
func (v *VSphere) templateForKubeVersionAndOSMachineConfig(name string, kubeVersion anywherev1.KubernetesVersion, os OS) api.VSphereFiller {
	useBundlesOverride := getBundlesOverride() == "true"
	template := v.templateForDevRelease(kubeVersion, os, useBundlesOverride)
	return api.WithMachineTemplate(name, template)
}

// Ubuntu128Template returns vsphere filler for 1.28 Ubuntu.
func (v *VSphere) Ubuntu128Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube128, Ubuntu2004, nil)
}

// Ubuntu129Template returns vsphere filler for 1.29 Ubuntu.
func (v *VSphere) Ubuntu129Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube129, Ubuntu2004, nil)
}

// Ubuntu130Template returns vsphere filler for 1.30 Ubuntu.
func (v *VSphere) Ubuntu130Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube130, Ubuntu2004, nil)
}

// Ubuntu131Template returns vsphere filler for 1.31 Ubuntu.
func (v *VSphere) Ubuntu131Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube131, Ubuntu2004, nil)
}

// Ubuntu132Template returns vsphere filler for 1.32 Ubuntu.
func (v *VSphere) Ubuntu132Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube132, Ubuntu2004, nil)
}

// Ubuntu133Template returns vsphere filler for 1.33 Ubuntu.
func (v *VSphere) Ubuntu133Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube133, Ubuntu2004, nil)
}

// Ubuntu129TemplateForMachineConfig returns vsphere filler for 1.29 Ubuntu for a specific machine config.
func (v *VSphere) Ubuntu129TemplateForMachineConfig(name string) api.VSphereFiller {
	return v.templateForKubeVersionAndOSMachineConfig(name, anywherev1.Kube129, Ubuntu2004)
}

// Ubuntu2204Kubernetes128Template returns vsphere filler for 1.28 Ubuntu 22.04.
func (v *VSphere) Ubuntu2204Kubernetes128Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube128, Ubuntu2204, nil)
}

// Ubuntu2204Kubernetes129Template returns vsphere filler for 1.29 Ubuntu 22.04.
func (v *VSphere) Ubuntu2204Kubernetes129Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube129, Ubuntu2204, nil)
}

// Ubuntu2204Kubernetes130Template returns vsphere filler for 1.30 Ubuntu 22.04.
func (v *VSphere) Ubuntu2204Kubernetes130Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube130, Ubuntu2204, nil)
}

// Ubuntu2204Kubernetes131Template returns vsphere filler for 1.31 Ubuntu 22.04.
func (v *VSphere) Ubuntu2204Kubernetes131Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube131, Ubuntu2204, nil)
}

// Ubuntu2204Kubernetes132Template returns vsphere filler for 1.32 Ubuntu 22.04.
func (v *VSphere) Ubuntu2204Kubernetes132Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube132, Ubuntu2204, nil)
}

// Ubuntu2204Kubernetes133Template returns vsphere filler for 1.33 Ubuntu 22.04.
func (v *VSphere) Ubuntu2204Kubernetes133Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube133, Ubuntu2204, nil)
}

// Bottlerocket128Template returns vsphere filler for 1.28 BR.
func (v *VSphere) Bottlerocket128Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube128, Bottlerocket1, nil)
}

// Bottlerocket129Template returns vsphere filler for 1.29 BR.
func (v *VSphere) Bottlerocket129Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube129, Bottlerocket1, nil)
}

// Bottlerocket130Template returns vsphere filler for 1.30 BR.
func (v *VSphere) Bottlerocket130Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube130, Bottlerocket1, nil)
}

// Bottlerocket131Template returns vsphere filler for 1.31 BR.
func (v *VSphere) Bottlerocket131Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube131, Bottlerocket1, nil)
}

// Bottlerocket132Template returns vsphere filler for 1.32 BR.
func (v *VSphere) Bottlerocket132Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube132, Bottlerocket1, nil)
}

// Bottlerocket133Template returns vsphere filler for 1.33 BR.
func (v *VSphere) Bottlerocket133Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube133, Bottlerocket1, nil)
}

// Redhat128Template returns vsphere filler for 1.28 Redhat.
func (v *VSphere) Redhat128Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube128, RedHat8, nil)
}

// Redhat129Template returns vsphere filler for 1.29 Redhat.
func (v *VSphere) Redhat129Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube129, RedHat8, nil)
}

// Redhat130Template returns vsphere filler for 1.30 Redhat.
func (v *VSphere) Redhat130Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube130, RedHat8, nil)
}

// Redhat131Template returns vsphere filler for 1.31 Redhat.
func (v *VSphere) Redhat131Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube131, RedHat8, nil)
}

// Redhat9128Template returns vsphere filler for 1.28 Redhat 9.
func (v *VSphere) Redhat9128Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube128, RedHat9, nil)
}

// Redhat9129Template returns vsphere filler for 1.29 Redhat 9.
func (v *VSphere) Redhat9129Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube129, RedHat9, nil)
}

// Redhat9130Template returns vsphere filler for 1.30 Redhat 9.
func (v *VSphere) Redhat9130Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube130, RedHat9, nil)
}

// Redhat9131Template returns vsphere filler for 1.31 Redhat 9.
func (v *VSphere) Redhat9131Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube131, RedHat9, nil)
}

// Redhat9132Template returns vsphere filler for 1.32 Redhat 9.
func (v *VSphere) Redhat9132Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube132, RedHat9, nil)
}

// Redhat9133Template returns vsphere filler for 1.33 Redhat 9.
func (v *VSphere) Redhat9133Template() api.VSphereFiller {
	return v.templateForKubeVersionAndOS(anywherev1.Kube133, RedHat9, nil)
}

func (v *VSphere) getDevRelease() *releasev1.EksARelease {
	v.t.Helper()
	if v.devRelease == nil {
		localDevRelease, err := localEksaCLIDevVersionRelease()
		if err != nil {
			v.t.Fatal(err)
		}
		v.t.Log("Using local eksa dev release:", localDevRelease.Version)
		v.devRelease = localDevRelease
	}

	return v.devRelease
}

func (v *VSphere) templateForDevRelease(kubeVersion anywherev1.KubernetesVersion, os OS, useBundlesOverride bool) string {
	v.t.Helper()
	return v.templatesRegistry.templateForRelease(v.t, v.getDevRelease(), kubeVersion, os, useBundlesOverride)
}

func RequiredVsphereEnvVars() []string {
	return requiredEnvVars
}

// VSphereExtraEnvVarPrefixes returns prefixes for env vars that although not always required,
// might be necessary for certain tests.
func VSphereExtraEnvVarPrefixes() []string {
	return []string{
		vsphereTemplateEnvVarPrefix,
	}
}

func vSphereMachineConfig(name string, fillers ...api.VSphereMachineConfigFiller) api.VSphereFiller {
	f := make([]api.VSphereMachineConfigFiller, 0, len(fillers)+6)
	// Need to add these because at this point the default fillers that assign these
	// values to all machines have already ran
	f = append(f,
		api.WithVSphereMachineDefaultValues(),
		api.WithDatastore(os.Getenv(vsphereDatastoreVar)),
		api.WithFolder(os.Getenv(vsphereFolderVar)),
		api.WithResourcePool(os.Getenv(vsphereResourcePoolVar)),
		api.WithStoragePolicyName(os.Getenv(vsphereStoragePolicyNameVar)),
		api.WithSSHKey(os.Getenv(vsphereSshAuthorizedKeyVar)),
	)
	f = append(f, fillers...)

	return api.WithVSphereMachineConfig(name, f...)
}

func buildVSphereWorkerNodeGroupClusterFiller(machineConfigName string, workerNodeGroup *WorkerNodeGroup) api.ClusterFiller {
	// Set worker node group ref to vsphere machine config
	workerNodeGroup.MachineConfigKind = anywherev1.VSphereMachineConfigKind
	workerNodeGroup.MachineConfigName = machineConfigName
	return workerNodeGroup.ClusterFiller()
}

// WithKubeVersionAndOSForRelease returns a vSphereOpt that sets the cluster kube version and the right template for all
// vsphere machine configs based on the EKS-A release.
func WithKubeVersionAndOSForRelease(kubeVersion anywherev1.KubernetesVersion, os OS, release *releasev1.EksARelease, useBundlesOverride bool) VSphereOpt {
	return optionToSetTemplateForRelease(kubeVersion, os, release, useBundlesOverride)
}

// WithKubeVersionAndOSForRelease returns a cluster config filler that sets the cluster kube version and the right template for all
// vsphere machine configs based on the EKS-A release.
func (v *VSphere) WithKubeVersionAndOSForRelease(kubeVersion anywherev1.KubernetesVersion, os OS, release *releasev1.EksARelease, useBundlesOverride bool) api.ClusterConfigFiller {
	return api.VSphereToConfigFiller(
		api.WithTemplateForAllMachines(v.templatesRegistry.templateForRelease(v.t, release, kubeVersion, os, useBundlesOverride)),
	)
}

func optionToSetTemplateForRelease(kubeVersion anywherev1.KubernetesVersion, os OS, release *releasev1.EksARelease, useBundlesOverride bool) VSphereOpt {
	return func(v *VSphere) {
		v.fillers = append(v.fillers,
			api.WithTemplateForAllMachines(v.templatesRegistry.templateForRelease(v.t, release, kubeVersion, os, useBundlesOverride)),
		)
	}
}

// envVarForTemplate looks for explicit configuration through an env var: "T_VSPHERE_TEMPLATE_{osFamily}_{eks-d version}"
// eg: T_VSPHERE_TEMPLATE_REDHAT_KUBERNETES_1_27_EKS_22.
func (v *VSphere) envVarForTemplate(os OS, eksDName string) string {
	return fmt.Sprintf("T_VSPHERE_TEMPLATE_%s_%s", strings.ToUpper(strings.ReplaceAll(string(os), "-", "_")), strings.ToUpper(strings.ReplaceAll(eksDName, "-", "_")))
}

// defaultNameForTemplate looks for a template with the name path: "{folder}/{eks-d version}-{osFamily}"
// eg: /SDDC-Datacenter/vm/Templates/kubernetes-1-27-eks-22-redhat.
func (v *VSphere) defaultNameForTemplate(os OS, eksDName string) string {
	folder := v.testsConfig.TemplatesFolder
	if folder == "" {
		v.t.Log("vSphere templates folder is not configured.")
		return ""
	}
	return filepath.Join(folder, fmt.Sprintf("%s-%s", strings.ToLower(eksDName), strings.ToLower(string(os))))
}

// defaultEnvVarForTemplate returns the value of the default template env vars: "T_VSPHERE_TEMPLATE_{osFamily}_{kubeVersion}"
// eg. T_VSPHERE_TEMPLATE_REDHAT_1_27.
func (v *VSphere) defaultEnvVarForTemplate(os OS, kubeVersion anywherev1.KubernetesVersion) string {
	if osFamiliesForOS[os] == anywherev1.Bottlerocket {
		os = OS(strings.ReplaceAll(string(os), "bottlerocket", "br"))
	}
	return fmt.Sprintf("T_VSPHERE_TEMPLATE_%s_%s", strings.ToUpper(strings.ReplaceAll(string(os), "-", "_")), strings.ReplaceAll(string(kubeVersion), ".", "_"))
}

// searchTemplate returns template name if the given template exists in the datacenter.
func (v *VSphere) searchTemplate(ctx context.Context, template string) (string, error) {
	foundTemplate, err := v.GovcClient.SearchTemplate(context.Background(), v.testsConfig.Datacenter, template)
	if err != nil {
		return "", err
	}
	return foundTemplate, nil
}

func readVersionsBundles(t testing.TB, release *releasev1.EksARelease, kubeVersion anywherev1.KubernetesVersion, useBundlesOverride bool) *releasev1.VersionsBundle {
	reader := newFileReader()
	var allBundles *releasev1.Bundles
	var err error
	if useBundlesOverride {
		allBundles, err = bundles.Read(reader, defaultBundleReleaseManifestFile)
		if err != nil {
			t.Fatal(err)
		}
	} else {
		allBundles, err = releases.ReadBundlesForRelease(reader, release)
		if err != nil {
			t.Fatal(err)
		}
	}

	return bundles.VersionsBundleForKubernetesVersion(allBundles, string(kubeVersion))
}

func readVSphereConfig() (vsphereConfig, error) {
	return vsphereConfig{
		Datacenter:        os.Getenv(vsphereDatacenterVar),
		Datastore:         os.Getenv(vsphereDatastoreVar),
		Folder:            os.Getenv(vsphereFolderVar),
		Network:           os.Getenv(vsphereNetworkVar),
		ResourcePool:      os.Getenv(vsphereResourcePoolVar),
		Server:            os.Getenv(vsphereServerVar),
		SSHAuthorizedKey:  os.Getenv(vsphereSshAuthorizedKeyVar),
		StoragePolicyName: os.Getenv(vsphereStoragePolicyNameVar),
		TLSInsecure:       os.Getenv(vsphereTlsInsecureVar) == "true",
		TLSThumbprint:     os.Getenv(vsphereTlsThumbprintVar),
		TemplatesFolder:   os.Getenv(vsphereTemplatesFolder),
	}, nil
}

// ClusterStateValidations returns a list of provider specific validations.
func (v *VSphere) ClusterStateValidations() []clusterf.StateValidation {
	return []clusterf.StateValidation{}
}

// ValidateNodesDiskGiB validates DiskGiB for all the machines.
func (v *VSphere) ValidateNodesDiskGiB(machines map[string]anywheretypes.Machine, expectedDiskSize int) error {
	v.t.Log("===================== Disk Size Validation Task =====================")
	for _, m := range machines {
		v.t.Log("Verifying disk size for VM", "Virtual Machine", m.Metadata.Name)
		diskSize, err := v.GovcClient.GetVMDiskSizeInGB(context.Background(), m.Metadata.Name, v.testsConfig.Datacenter)
		if err != nil {
			v.t.Fatalf("validating disk size: %v", err)
		}

		v.t.Log("Disk Size in GiB", "Expected", expectedDiskSize, "Actual", diskSize)
		if diskSize != expectedDiskSize {
			v.t.Fatalf("diskGib for node %s did not match the expected disk size. Expected=%dGiB, Actual=%dGiB", m.Metadata.Name, expectedDiskSize, diskSize)
		}
	}
	return nil
}
