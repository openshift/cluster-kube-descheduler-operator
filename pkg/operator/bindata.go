// Code generated for package operator by go-bindata DO NOT EDIT. (@generated)
// sources:
// pkg/operator/testdata/affinityAndTaintsWithNamespaces.yaml
// pkg/operator/testdata/highNodeUtilization.yaml
// pkg/operator/testdata/highNodeUtilizationMinimal.yaml
// pkg/operator/testdata/highNodeUtilizationModerate.yaml
// pkg/operator/testdata/highNodeUtilizationWithNamespaces.yaml
// pkg/operator/testdata/lifecycleAndUtilizationEvictPvcPodsConfig.yaml
// pkg/operator/testdata/lifecycleAndUtilizationPodLifeTimeCustomizationConfig.yaml
// pkg/operator/testdata/lifecycleAndUtilizationPodLifeTimeWithThresholdPriorityClassNameConfig.yaml
// pkg/operator/testdata/lifecycleAndUtilizationPodLifeTimeWithThresholdPriorityConfig.yaml
// pkg/operator/testdata/longLifecycleWithLocalStorage.yaml
// pkg/operator/testdata/longLifecycleWithNamespaces.yaml
// pkg/operator/testdata/lowNodeUtilizationHighConfig.yaml
// pkg/operator/testdata/lowNodeUtilizationLowConfig.yaml
// pkg/operator/testdata/lowNodeUtilizationMediumConfig.yaml
// pkg/operator/testdata/softTopologyAndDuplicates.yaml
// pkg/operator/testdata/topologyAndDuplicates.yaml
package operator

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type asset struct {
	bytes []byte
	info  os.FileInfo
}

type bindataFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
}

// Name return file name
func (fi bindataFileInfo) Name() string {
	return fi.name
}

// Size return file size
func (fi bindataFileInfo) Size() int64 {
	return fi.size
}

// Mode return file mode
func (fi bindataFileInfo) Mode() os.FileMode {
	return fi.mode
}

// Mode return file modify time
func (fi bindataFileInfo) ModTime() time.Time {
	return fi.modTime
}

// IsDir return file whether a directory
func (fi bindataFileInfo) IsDir() bool {
	return fi.mode&os.ModeDir != 0
}

// Sys return file is sys mode
func (fi bindataFileInfo) Sys() interface{} {
	return nil
}

var _pkgOperatorTestdataAffinityandtaintswithnamespacesYaml = []byte(`apiVersion: descheduler/v1alpha2
kind: DeschedulerPolicy
profiles:
- name: AffinityAndTaints
  pluginConfig:
  - args:
      labelSelector: null
      namespaces:
        exclude: []
        include:
        - includedNamespace
    name: RemovePodsViolatingInterPodAntiAffinity
  - args:
      excludedTaints: null
      includePreferNoSchedule: false
      includedTaints: null
      labelSelector: null
      namespaces:
        exclude: []
        include:
        - includedNamespace
    name: RemovePodsViolatingNodeTaints
  - args:
      labelSelector: null
      namespaces:
        exclude: []
        include:
        - includedNamespace
      nodeAffinityType:
      - requiredDuringSchedulingIgnoredDuringExecution
    name: RemovePodsViolatingNodeAffinity
  - args:
      evictDaemonSetPods: false
      evictFailedBarePods: false
      evictLocalStoragePods: false
      evictSystemCriticalPods: false
      ignorePvcPods: true
      labelSelector: null
      minPodAge: null
      minReplicas: 0
      nodeFit: false
      nodeSelector: ""
      priorityThreshold: null
    name: DefaultEvictor
  plugins:
    balance:
      disabled: null
      enabled: null
    deschedule:
      disabled: null
      enabled:
      - RemovePodsViolatingInterPodAntiAffinity
      - RemovePodsViolatingNodeTaints
      - RemovePodsViolatingNodeAffinity
    filter:
      disabled: null
      enabled:
      - DefaultEvictor
    preevictionfilter:
      disabled: null
      enabled: null
    presort:
      disabled: null
      enabled: null
    sort:
      disabled: null
      enabled: null
`)

func pkgOperatorTestdataAffinityandtaintswithnamespacesYamlBytes() ([]byte, error) {
	return _pkgOperatorTestdataAffinityandtaintswithnamespacesYaml, nil
}

func pkgOperatorTestdataAffinityandtaintswithnamespacesYaml() (*asset, error) {
	bytes, err := pkgOperatorTestdataAffinityandtaintswithnamespacesYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "pkg/operator/testdata/affinityAndTaintsWithNamespaces.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _pkgOperatorTestdataHighnodeutilizationYaml = []byte(`apiVersion: descheduler/v1alpha2
kind: DeschedulerPolicy
profiles:
- name: CompactAndScale
  pluginConfig:
  - args:
      evictableNamespaces:
        exclude:
        - openshift-kube-scheduler
        - kube-system
        include: null
      numberOfNodes: 0
      thresholds:
        cpu: 20
        memory: 20
        pods: 20
    name: HighNodeUtilization
  - args:
      evictDaemonSetPods: false
      evictFailedBarePods: false
      evictLocalStoragePods: false
      evictSystemCriticalPods: false
      ignorePvcPods: true
      labelSelector: null
      minPodAge: null
      minReplicas: 0
      nodeFit: false
      nodeSelector: ""
      priorityThreshold: null
    name: DefaultEvictor
  plugins:
    balance:
      disabled: null
      enabled:
      - HighNodeUtilization
    deschedule:
      disabled: null
      enabled: null
    filter:
      disabled: null
      enabled:
      - DefaultEvictor
    preevictionfilter:
      disabled: null
      enabled: null
    presort:
      disabled: null
      enabled: null
    sort:
      disabled: null
      enabled: null
`)

func pkgOperatorTestdataHighnodeutilizationYamlBytes() ([]byte, error) {
	return _pkgOperatorTestdataHighnodeutilizationYaml, nil
}

func pkgOperatorTestdataHighnodeutilizationYaml() (*asset, error) {
	bytes, err := pkgOperatorTestdataHighnodeutilizationYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "pkg/operator/testdata/highNodeUtilization.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _pkgOperatorTestdataHighnodeutilizationminimalYaml = []byte(`apiVersion: descheduler/v1alpha2
kind: DeschedulerPolicy
profiles:
- name: CompactAndScale
  pluginConfig:
  - args:
      evictableNamespaces:
        exclude:
        - openshift-kube-scheduler
        - kube-system
        include: null
      numberOfNodes: 0
      thresholds:
        cpu: 10
        memory: 10
        pods: 10
    name: HighNodeUtilization
  - args:
      evictDaemonSetPods: false
      evictFailedBarePods: false
      evictLocalStoragePods: false
      evictSystemCriticalPods: false
      ignorePvcPods: true
      labelSelector: null
      minPodAge: null
      minReplicas: 0
      nodeFit: false
      nodeSelector: ""
      priorityThreshold: null
    name: DefaultEvictor
  plugins:
    balance:
      disabled: null
      enabled:
      - HighNodeUtilization
    deschedule:
      disabled: null
      enabled: null
    filter:
      disabled: null
      enabled:
      - DefaultEvictor
    preevictionfilter:
      disabled: null
      enabled: null
    presort:
      disabled: null
      enabled: null
    sort:
      disabled: null
      enabled: null
`)

func pkgOperatorTestdataHighnodeutilizationminimalYamlBytes() ([]byte, error) {
	return _pkgOperatorTestdataHighnodeutilizationminimalYaml, nil
}

func pkgOperatorTestdataHighnodeutilizationminimalYaml() (*asset, error) {
	bytes, err := pkgOperatorTestdataHighnodeutilizationminimalYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "pkg/operator/testdata/highNodeUtilizationMinimal.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _pkgOperatorTestdataHighnodeutilizationmoderateYaml = []byte(`apiVersion: descheduler/v1alpha2
kind: DeschedulerPolicy
profiles:
- name: CompactAndScale
  pluginConfig:
  - args:
      evictableNamespaces:
        exclude:
        - openshift-kube-scheduler
        - kube-system
        include: null
      numberOfNodes: 0
      thresholds:
        cpu: 30
        memory: 30
        pods: 30
    name: HighNodeUtilization
  - args:
      evictDaemonSetPods: false
      evictFailedBarePods: false
      evictLocalStoragePods: false
      evictSystemCriticalPods: false
      ignorePvcPods: true
      labelSelector: null
      minPodAge: null
      minReplicas: 0
      nodeFit: false
      nodeSelector: ""
      priorityThreshold: null
    name: DefaultEvictor
  plugins:
    balance:
      disabled: null
      enabled:
      - HighNodeUtilization
    deschedule:
      disabled: null
      enabled: null
    filter:
      disabled: null
      enabled:
      - DefaultEvictor
    preevictionfilter:
      disabled: null
      enabled: null
    presort:
      disabled: null
      enabled: null
    sort:
      disabled: null
      enabled: null
`)

func pkgOperatorTestdataHighnodeutilizationmoderateYamlBytes() ([]byte, error) {
	return _pkgOperatorTestdataHighnodeutilizationmoderateYaml, nil
}

func pkgOperatorTestdataHighnodeutilizationmoderateYaml() (*asset, error) {
	bytes, err := pkgOperatorTestdataHighnodeutilizationmoderateYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "pkg/operator/testdata/highNodeUtilizationModerate.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _pkgOperatorTestdataHighnodeutilizationwithnamespacesYaml = []byte(`apiVersion: descheduler/v1alpha2
kind: DeschedulerPolicy
profiles:
- name: CompactAndScale
  pluginConfig:
  - args:
      evictableNamespaces: null
      numberOfNodes: 0
      thresholds:
        cpu: 20
        memory: 20
        pods: 20
    name: HighNodeUtilization
  - args:
      evictDaemonSetPods: false
      evictFailedBarePods: false
      evictLocalStoragePods: false
      evictSystemCriticalPods: false
      ignorePvcPods: true
      labelSelector: null
      minPodAge: null
      minReplicas: 0
      nodeFit: false
      nodeSelector: ""
      priorityThreshold: null
    name: DefaultEvictor
  plugins:
    balance:
      disabled: null
      enabled:
      - HighNodeUtilization
    deschedule:
      disabled: null
      enabled: null
    filter:
      disabled: null
      enabled:
      - DefaultEvictor
    preevictionfilter:
      disabled: null
      enabled: null
    presort:
      disabled: null
      enabled: null
    sort:
      disabled: null
      enabled: null
`)

func pkgOperatorTestdataHighnodeutilizationwithnamespacesYamlBytes() ([]byte, error) {
	return _pkgOperatorTestdataHighnodeutilizationwithnamespacesYaml, nil
}

func pkgOperatorTestdataHighnodeutilizationwithnamespacesYaml() (*asset, error) {
	bytes, err := pkgOperatorTestdataHighnodeutilizationwithnamespacesYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "pkg/operator/testdata/highNodeUtilizationWithNamespaces.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _pkgOperatorTestdataLifecycleandutilizationevictpvcpodsconfigYaml = []byte(`apiVersion: descheduler/v1alpha2
kind: DeschedulerPolicy
profiles:
- name: LifecycleAndUtilization
  pluginConfig:
  - args:
      includingEphemeralContainers: false
      includingInitContainers: false
      labelSelector: null
      maxPodLifeTimeSeconds: 86400
      namespaces: null
      states: null
    name: PodLifeTime
  - args:
      includingInitContainers: true
      labelSelector: null
      namespaces: null
      podRestartThreshold: 100
      states: null
    name: RemovePodsHavingTooManyRestarts
  - args:
      evictableNamespaces: null
      numberOfNodes: 0
      targetThresholds:
        cpu: 50
        memory: 50
        pods: 50
      thresholds:
        cpu: 20
        memory: 20
        pods: 20
      useDeviationThresholds: false
    name: LowNodeUtilization
  - args:
      evictDaemonSetPods: false
      evictFailedBarePods: false
      evictLocalStoragePods: false
      evictSystemCriticalPods: false
      ignorePvcPods: false
      labelSelector: null
      minPodAge: null
      minReplicas: 0
      nodeFit: false
      nodeSelector: ""
      priorityThreshold: null
    name: DefaultEvictor
  plugins:
    balance:
      disabled: null
      enabled:
      - LowNodeUtilization
    deschedule:
      disabled: null
      enabled:
      - PodLifeTime
      - RemovePodsHavingTooManyRestarts
    filter:
      disabled: null
      enabled:
      - DefaultEvictor
    preevictionfilter:
      disabled: null
      enabled: null
    presort:
      disabled: null
      enabled: null
    sort:
      disabled: null
      enabled: null
`)

func pkgOperatorTestdataLifecycleandutilizationevictpvcpodsconfigYamlBytes() ([]byte, error) {
	return _pkgOperatorTestdataLifecycleandutilizationevictpvcpodsconfigYaml, nil
}

func pkgOperatorTestdataLifecycleandutilizationevictpvcpodsconfigYaml() (*asset, error) {
	bytes, err := pkgOperatorTestdataLifecycleandutilizationevictpvcpodsconfigYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "pkg/operator/testdata/lifecycleAndUtilizationEvictPvcPodsConfig.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _pkgOperatorTestdataLifecycleandutilizationpodlifetimecustomizationconfigYaml = []byte(`apiVersion: descheduler/v1alpha2
kind: DeschedulerPolicy
profiles:
- name: LifecycleAndUtilization
  pluginConfig:
  - args:
      includingEphemeralContainers: false
      includingInitContainers: false
      labelSelector: null
      maxPodLifeTimeSeconds: 300
      namespaces: null
      states: null
    name: PodLifeTime
  - args:
      includingInitContainers: true
      labelSelector: null
      namespaces: null
      podRestartThreshold: 100
      states: null
    name: RemovePodsHavingTooManyRestarts
  - args:
      evictableNamespaces: null
      numberOfNodes: 0
      targetThresholds:
        cpu: 50
        memory: 50
        pods: 50
      thresholds:
        cpu: 20
        memory: 20
        pods: 20
      useDeviationThresholds: false
    name: LowNodeUtilization
  - args:
      evictDaemonSetPods: false
      evictFailedBarePods: false
      evictLocalStoragePods: false
      evictSystemCriticalPods: false
      ignorePvcPods: true
      labelSelector: null
      minPodAge: null
      minReplicas: 0
      nodeFit: false
      nodeSelector: ""
      priorityThreshold: null
    name: DefaultEvictor
  plugins:
    balance:
      disabled: null
      enabled:
      - LowNodeUtilization
    deschedule:
      disabled: null
      enabled:
      - PodLifeTime
      - RemovePodsHavingTooManyRestarts
    filter:
      disabled: null
      enabled:
      - DefaultEvictor
    preevictionfilter:
      disabled: null
      enabled: null
    presort:
      disabled: null
      enabled: null
    sort:
      disabled: null
      enabled: null
`)

func pkgOperatorTestdataLifecycleandutilizationpodlifetimecustomizationconfigYamlBytes() ([]byte, error) {
	return _pkgOperatorTestdataLifecycleandutilizationpodlifetimecustomizationconfigYaml, nil
}

func pkgOperatorTestdataLifecycleandutilizationpodlifetimecustomizationconfigYaml() (*asset, error) {
	bytes, err := pkgOperatorTestdataLifecycleandutilizationpodlifetimecustomizationconfigYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "pkg/operator/testdata/lifecycleAndUtilizationPodLifeTimeCustomizationConfig.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _pkgOperatorTestdataLifecycleandutilizationpodlifetimewiththresholdpriorityclassnameconfigYaml = []byte(`apiVersion: descheduler/v1alpha2
kind: DeschedulerPolicy
profiles:
- name: LifecycleAndUtilization
  pluginConfig:
  - args:
      includingEphemeralContainers: false
      includingInitContainers: false
      labelSelector: null
      maxPodLifeTimeSeconds: 86400
      namespaces: null
      states: null
    name: PodLifeTime
  - args:
      includingInitContainers: true
      labelSelector: null
      namespaces: null
      podRestartThreshold: 100
      states: null
    name: RemovePodsHavingTooManyRestarts
  - args:
      evictableNamespaces: null
      numberOfNodes: 0
      targetThresholds:
        cpu: 50
        memory: 50
        pods: 50
      thresholds:
        cpu: 20
        memory: 20
        pods: 20
      useDeviationThresholds: false
    name: LowNodeUtilization
  - args:
      evictDaemonSetPods: false
      evictFailedBarePods: false
      evictLocalStoragePods: false
      evictSystemCriticalPods: false
      ignorePvcPods: true
      labelSelector: null
      minPodAge: null
      minReplicas: 0
      nodeFit: false
      nodeSelector: ""
      priorityThreshold:
        name: className
        value: null
    name: DefaultEvictor
  plugins:
    balance:
      disabled: null
      enabled:
      - LowNodeUtilization
    deschedule:
      disabled: null
      enabled:
      - PodLifeTime
      - RemovePodsHavingTooManyRestarts
    filter:
      disabled: null
      enabled:
      - DefaultEvictor
    preevictionfilter:
      disabled: null
      enabled: null
    presort:
      disabled: null
      enabled: null
    sort:
      disabled: null
      enabled: null
`)

func pkgOperatorTestdataLifecycleandutilizationpodlifetimewiththresholdpriorityclassnameconfigYamlBytes() ([]byte, error) {
	return _pkgOperatorTestdataLifecycleandutilizationpodlifetimewiththresholdpriorityclassnameconfigYaml, nil
}

func pkgOperatorTestdataLifecycleandutilizationpodlifetimewiththresholdpriorityclassnameconfigYaml() (*asset, error) {
	bytes, err := pkgOperatorTestdataLifecycleandutilizationpodlifetimewiththresholdpriorityclassnameconfigYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "pkg/operator/testdata/lifecycleAndUtilizationPodLifeTimeWithThresholdPriorityClassNameConfig.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _pkgOperatorTestdataLifecycleandutilizationpodlifetimewiththresholdpriorityconfigYaml = []byte(`apiVersion: descheduler/v1alpha2
kind: DeschedulerPolicy
profiles:
- name: LifecycleAndUtilization
  pluginConfig:
  - args:
      includingEphemeralContainers: false
      includingInitContainers: false
      labelSelector: null
      maxPodLifeTimeSeconds: 86400
      namespaces: null
      states: null
    name: PodLifeTime
  - args:
      includingInitContainers: true
      labelSelector: null
      namespaces: null
      podRestartThreshold: 100
      states: null
    name: RemovePodsHavingTooManyRestarts
  - args:
      evictableNamespaces: null
      numberOfNodes: 0
      targetThresholds:
        cpu: 50
        memory: 50
        pods: 50
      thresholds:
        cpu: 20
        memory: 20
        pods: 20
      useDeviationThresholds: false
    name: LowNodeUtilization
  - args:
      evictDaemonSetPods: false
      evictFailedBarePods: false
      evictLocalStoragePods: false
      evictSystemCriticalPods: false
      ignorePvcPods: true
      labelSelector: null
      minPodAge: null
      minReplicas: 0
      nodeFit: false
      nodeSelector: ""
      priorityThreshold:
        name: ""
        value: 1000
    name: DefaultEvictor
  plugins:
    balance:
      disabled: null
      enabled:
      - LowNodeUtilization
    deschedule:
      disabled: null
      enabled:
      - PodLifeTime
      - RemovePodsHavingTooManyRestarts
    filter:
      disabled: null
      enabled:
      - DefaultEvictor
    preevictionfilter:
      disabled: null
      enabled: null
    presort:
      disabled: null
      enabled: null
    sort:
      disabled: null
      enabled: null
`)

func pkgOperatorTestdataLifecycleandutilizationpodlifetimewiththresholdpriorityconfigYamlBytes() ([]byte, error) {
	return _pkgOperatorTestdataLifecycleandutilizationpodlifetimewiththresholdpriorityconfigYaml, nil
}

func pkgOperatorTestdataLifecycleandutilizationpodlifetimewiththresholdpriorityconfigYaml() (*asset, error) {
	bytes, err := pkgOperatorTestdataLifecycleandutilizationpodlifetimewiththresholdpriorityconfigYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "pkg/operator/testdata/lifecycleAndUtilizationPodLifeTimeWithThresholdPriorityConfig.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _pkgOperatorTestdataLonglifecyclewithlocalstorageYaml = []byte(`apiVersion: descheduler/v1alpha2
kind: DeschedulerPolicy
profiles:
- name: LongLifecycle
  pluginConfig:
  - args:
      includingInitContainers: true
      labelSelector: null
      namespaces:
        exclude:
        - openshift-kube-scheduler
        - kube-system
        include: []
      podRestartThreshold: 100
      states: null
    name: RemovePodsHavingTooManyRestarts
  - args:
      evictableNamespaces:
        exclude:
        - openshift-kube-scheduler
        - kube-system
        include: null
      numberOfNodes: 0
      targetThresholds:
        cpu: 50
        memory: 50
        pods: 50
      thresholds:
        cpu: 20
        memory: 20
        pods: 20
      useDeviationThresholds: false
    name: LowNodeUtilization
  - args:
      evictDaemonSetPods: false
      evictFailedBarePods: false
      evictLocalStoragePods: true
      evictSystemCriticalPods: false
      ignorePvcPods: true
      labelSelector: null
      minPodAge: null
      minReplicas: 0
      nodeFit: false
      nodeSelector: ""
      priorityThreshold: null
    name: DefaultEvictor
  plugins:
    balance:
      disabled: null
      enabled:
      - LowNodeUtilization
    deschedule:
      disabled: null
      enabled:
      - RemovePodsHavingTooManyRestarts
    filter:
      disabled: null
      enabled:
      - DefaultEvictor
    preevictionfilter:
      disabled: null
      enabled: null
    presort:
      disabled: null
      enabled: null
    sort:
      disabled: null
      enabled: null
`)

func pkgOperatorTestdataLonglifecyclewithlocalstorageYamlBytes() ([]byte, error) {
	return _pkgOperatorTestdataLonglifecyclewithlocalstorageYaml, nil
}

func pkgOperatorTestdataLonglifecyclewithlocalstorageYaml() (*asset, error) {
	bytes, err := pkgOperatorTestdataLonglifecyclewithlocalstorageYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "pkg/operator/testdata/longLifecycleWithLocalStorage.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _pkgOperatorTestdataLonglifecyclewithnamespacesYaml = []byte(`apiVersion: descheduler/v1alpha2
kind: DeschedulerPolicy
profiles:
- name: LongLifecycle
  pluginConfig:
  - args:
      includingInitContainers: true
      labelSelector: null
      namespaces:
        exclude: []
        include:
        - includedNamespace
      podRestartThreshold: 100
      states: null
    name: RemovePodsHavingTooManyRestarts
  - args:
      evictableNamespaces: null
      numberOfNodes: 0
      targetThresholds:
        cpu: 50
        memory: 50
        pods: 50
      thresholds:
        cpu: 20
        memory: 20
        pods: 20
      useDeviationThresholds: false
    name: LowNodeUtilization
  - args:
      evictDaemonSetPods: false
      evictFailedBarePods: false
      evictLocalStoragePods: false
      evictSystemCriticalPods: false
      ignorePvcPods: true
      labelSelector: null
      minPodAge: null
      minReplicas: 0
      nodeFit: false
      nodeSelector: ""
      priorityThreshold: null
    name: DefaultEvictor
  plugins:
    balance:
      disabled: null
      enabled:
      - LowNodeUtilization
    deschedule:
      disabled: null
      enabled:
      - RemovePodsHavingTooManyRestarts
    filter:
      disabled: null
      enabled:
      - DefaultEvictor
    preevictionfilter:
      disabled: null
      enabled: null
    presort:
      disabled: null
      enabled: null
    sort:
      disabled: null
      enabled: null
`)

func pkgOperatorTestdataLonglifecyclewithnamespacesYamlBytes() ([]byte, error) {
	return _pkgOperatorTestdataLonglifecyclewithnamespacesYaml, nil
}

func pkgOperatorTestdataLonglifecyclewithnamespacesYaml() (*asset, error) {
	bytes, err := pkgOperatorTestdataLonglifecyclewithnamespacesYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "pkg/operator/testdata/longLifecycleWithNamespaces.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _pkgOperatorTestdataLownodeutilizationhighconfigYaml = []byte(`apiVersion: descheduler/v1alpha2
kind: DeschedulerPolicy
profiles:
- name: LifecycleAndUtilization
  pluginConfig:
  - args:
      includingEphemeralContainers: false
      includingInitContainers: false
      labelSelector: null
      maxPodLifeTimeSeconds: 86400
      namespaces: null
      states: null
    name: PodLifeTime
  - args:
      includingInitContainers: true
      labelSelector: null
      namespaces: null
      podRestartThreshold: 100
      states: null
    name: RemovePodsHavingTooManyRestarts
  - args:
      evictableNamespaces: null
      numberOfNodes: 0
      targetThresholds:
        cpu: 70
        memory: 70
        pods: 70
      thresholds:
        cpu: 40
        memory: 40
        pods: 40
      useDeviationThresholds: false
    name: LowNodeUtilization
  - args:
      evictDaemonSetPods: false
      evictFailedBarePods: false
      evictLocalStoragePods: false
      evictSystemCriticalPods: false
      ignorePvcPods: true
      labelSelector: null
      minPodAge: null
      minReplicas: 0
      nodeFit: false
      nodeSelector: ""
      priorityThreshold: null
    name: DefaultEvictor
  plugins:
    balance:
      disabled: null
      enabled:
      - LowNodeUtilization
    deschedule:
      disabled: null
      enabled:
      - PodLifeTime
      - RemovePodsHavingTooManyRestarts
    filter:
      disabled: null
      enabled:
      - DefaultEvictor
    preevictionfilter:
      disabled: null
      enabled: null
    presort:
      disabled: null
      enabled: null
    sort:
      disabled: null
      enabled: null
`)

func pkgOperatorTestdataLownodeutilizationhighconfigYamlBytes() ([]byte, error) {
	return _pkgOperatorTestdataLownodeutilizationhighconfigYaml, nil
}

func pkgOperatorTestdataLownodeutilizationhighconfigYaml() (*asset, error) {
	bytes, err := pkgOperatorTestdataLownodeutilizationhighconfigYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "pkg/operator/testdata/lowNodeUtilizationHighConfig.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _pkgOperatorTestdataLownodeutilizationlowconfigYaml = []byte(`apiVersion: descheduler/v1alpha2
kind: DeschedulerPolicy
profiles:
- name: LifecycleAndUtilization
  pluginConfig:
  - args:
      includingEphemeralContainers: false
      includingInitContainers: false
      labelSelector: null
      maxPodLifeTimeSeconds: 86400
      namespaces: null
      states: null
    name: PodLifeTime
  - args:
      includingInitContainers: true
      labelSelector: null
      namespaces: null
      podRestartThreshold: 100
      states: null
    name: RemovePodsHavingTooManyRestarts
  - args:
      evictableNamespaces: null
      numberOfNodes: 0
      targetThresholds:
        cpu: 30
        memory: 30
        pods: 30
      thresholds:
        cpu: 10
        memory: 10
        pods: 10
      useDeviationThresholds: false
    name: LowNodeUtilization
  - args:
      evictDaemonSetPods: false
      evictFailedBarePods: false
      evictLocalStoragePods: false
      evictSystemCriticalPods: false
      ignorePvcPods: true
      labelSelector: null
      minPodAge: null
      minReplicas: 0
      nodeFit: false
      nodeSelector: ""
      priorityThreshold: null
    name: DefaultEvictor
  plugins:
    balance:
      disabled: null
      enabled:
      - LowNodeUtilization
    deschedule:
      disabled: null
      enabled:
      - PodLifeTime
      - RemovePodsHavingTooManyRestarts
    filter:
      disabled: null
      enabled:
      - DefaultEvictor
    preevictionfilter:
      disabled: null
      enabled: null
    presort:
      disabled: null
      enabled: null
    sort:
      disabled: null
      enabled: null
`)

func pkgOperatorTestdataLownodeutilizationlowconfigYamlBytes() ([]byte, error) {
	return _pkgOperatorTestdataLownodeutilizationlowconfigYaml, nil
}

func pkgOperatorTestdataLownodeutilizationlowconfigYaml() (*asset, error) {
	bytes, err := pkgOperatorTestdataLownodeutilizationlowconfigYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "pkg/operator/testdata/lowNodeUtilizationLowConfig.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _pkgOperatorTestdataLownodeutilizationmediumconfigYaml = []byte(`apiVersion: descheduler/v1alpha2
kind: DeschedulerPolicy
profiles:
- name: LifecycleAndUtilization
  pluginConfig:
  - args:
      includingEphemeralContainers: false
      includingInitContainers: false
      labelSelector: null
      maxPodLifeTimeSeconds: 86400
      namespaces: null
      states: null
    name: PodLifeTime
  - args:
      includingInitContainers: true
      labelSelector: null
      namespaces: null
      podRestartThreshold: 100
      states: null
    name: RemovePodsHavingTooManyRestarts
  - args:
      evictableNamespaces: null
      numberOfNodes: 0
      targetThresholds:
        cpu: 50
        memory: 50
        pods: 50
      thresholds:
        cpu: 20
        memory: 20
        pods: 20
      useDeviationThresholds: false
    name: LowNodeUtilization
  - args:
      evictDaemonSetPods: false
      evictFailedBarePods: false
      evictLocalStoragePods: false
      evictSystemCriticalPods: false
      ignorePvcPods: true
      labelSelector: null
      minPodAge: null
      minReplicas: 0
      nodeFit: false
      nodeSelector: ""
      priorityThreshold: null
    name: DefaultEvictor
  plugins:
    balance:
      disabled: null
      enabled:
      - LowNodeUtilization
    deschedule:
      disabled: null
      enabled:
      - PodLifeTime
      - RemovePodsHavingTooManyRestarts
    filter:
      disabled: null
      enabled:
      - DefaultEvictor
    preevictionfilter:
      disabled: null
      enabled: null
    presort:
      disabled: null
      enabled: null
    sort:
      disabled: null
      enabled: null
`)

func pkgOperatorTestdataLownodeutilizationmediumconfigYamlBytes() ([]byte, error) {
	return _pkgOperatorTestdataLownodeutilizationmediumconfigYaml, nil
}

func pkgOperatorTestdataLownodeutilizationmediumconfigYaml() (*asset, error) {
	bytes, err := pkgOperatorTestdataLownodeutilizationmediumconfigYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "pkg/operator/testdata/lowNodeUtilizationMediumConfig.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _pkgOperatorTestdataSofttopologyandduplicatesYaml = []byte(`apiVersion: descheduler/v1alpha2
kind: DeschedulerPolicy
profiles:
- name: SoftTopologyAndDuplicates
  pluginConfig:
  - args:
      constraints:
      - DoNotSchedule
      - ScheduleAnyway
      labelSelector: null
      namespaces:
        exclude:
        - openshift-kube-scheduler
        - kube-system
        include: []
      topologyBalanceNodeFit: null
    name: RemovePodsViolatingTopologySpreadConstraint
  - args:
      excludeOwnerKinds: null
      namespaces:
        exclude:
        - openshift-kube-scheduler
        - kube-system
        include: []
    name: RemoveDuplicates
  - args:
      evictDaemonSetPods: false
      evictFailedBarePods: false
      evictLocalStoragePods: false
      evictSystemCriticalPods: false
      ignorePvcPods: true
      labelSelector: null
      minPodAge: null
      minReplicas: 0
      nodeFit: false
      nodeSelector: ""
      priorityThreshold: null
    name: DefaultEvictor
  plugins:
    balance:
      disabled: null
      enabled:
      - RemovePodsViolatingTopologySpreadConstraint
      - RemoveDuplicates
    deschedule:
      disabled: null
      enabled: null
    filter:
      disabled: null
      enabled:
      - DefaultEvictor
    preevictionfilter:
      disabled: null
      enabled: null
    presort:
      disabled: null
      enabled: null
    sort:
      disabled: null
      enabled: null
`)

func pkgOperatorTestdataSofttopologyandduplicatesYamlBytes() ([]byte, error) {
	return _pkgOperatorTestdataSofttopologyandduplicatesYaml, nil
}

func pkgOperatorTestdataSofttopologyandduplicatesYaml() (*asset, error) {
	bytes, err := pkgOperatorTestdataSofttopologyandduplicatesYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "pkg/operator/testdata/softTopologyAndDuplicates.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _pkgOperatorTestdataTopologyandduplicatesYaml = []byte(`apiVersion: descheduler/v1alpha2
kind: DeschedulerPolicy
profiles:
- name: TopologyAndDuplicates
  pluginConfig:
  - args:
      constraints:
      - DoNotSchedule
      labelSelector: null
      namespaces:
        exclude:
        - openshift-kube-scheduler
        - kube-system
        include: []
      topologyBalanceNodeFit: null
    name: RemovePodsViolatingTopologySpreadConstraint
  - args:
      excludeOwnerKinds: null
      namespaces:
        exclude:
        - openshift-kube-scheduler
        - kube-system
        include: []
    name: RemoveDuplicates
  - args:
      evictDaemonSetPods: false
      evictFailedBarePods: false
      evictLocalStoragePods: false
      evictSystemCriticalPods: false
      ignorePvcPods: true
      labelSelector: null
      minPodAge: null
      minReplicas: 0
      nodeFit: false
      nodeSelector: ""
      priorityThreshold: null
    name: DefaultEvictor
  plugins:
    balance:
      disabled: null
      enabled:
      - RemovePodsViolatingTopologySpreadConstraint
      - RemoveDuplicates
    deschedule:
      disabled: null
      enabled: null
    filter:
      disabled: null
      enabled:
      - DefaultEvictor
    preevictionfilter:
      disabled: null
      enabled: null
    presort:
      disabled: null
      enabled: null
    sort:
      disabled: null
      enabled: null
`)

func pkgOperatorTestdataTopologyandduplicatesYamlBytes() ([]byte, error) {
	return _pkgOperatorTestdataTopologyandduplicatesYaml, nil
}

func pkgOperatorTestdataTopologyandduplicatesYaml() (*asset, error) {
	bytes, err := pkgOperatorTestdataTopologyandduplicatesYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "pkg/operator/testdata/topologyAndDuplicates.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

// Asset loads and returns the asset for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func Asset(name string) ([]byte, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("Asset %s can't read by error: %v", name, err)
		}
		return a.bytes, nil
	}
	return nil, fmt.Errorf("Asset %s not found", name)
}

// MustAsset is like Asset but panics when Asset would return an error.
// It simplifies safe initialization of global variables.
func MustAsset(name string) []byte {
	a, err := Asset(name)
	if err != nil {
		panic("asset: Asset(" + name + "): " + err.Error())
	}

	return a
}

// AssetInfo loads and returns the asset info for the given name.
// It returns an error if the asset could not be found or
// could not be loaded.
func AssetInfo(name string) (os.FileInfo, error) {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	if f, ok := _bindata[cannonicalName]; ok {
		a, err := f()
		if err != nil {
			return nil, fmt.Errorf("AssetInfo %s can't read by error: %v", name, err)
		}
		return a.info, nil
	}
	return nil, fmt.Errorf("AssetInfo %s not found", name)
}

// AssetNames returns the names of the assets.
func AssetNames() []string {
	names := make([]string, 0, len(_bindata))
	for name := range _bindata {
		names = append(names, name)
	}
	return names
}

// _bindata is a table, holding each asset generator, mapped to its name.
var _bindata = map[string]func() (*asset, error){
	"pkg/operator/testdata/affinityAndTaintsWithNamespaces.yaml":                                        pkgOperatorTestdataAffinityandtaintswithnamespacesYaml,
	"pkg/operator/testdata/highNodeUtilization.yaml":                                                    pkgOperatorTestdataHighnodeutilizationYaml,
	"pkg/operator/testdata/highNodeUtilizationMinimal.yaml":                                             pkgOperatorTestdataHighnodeutilizationminimalYaml,
	"pkg/operator/testdata/highNodeUtilizationModerate.yaml":                                            pkgOperatorTestdataHighnodeutilizationmoderateYaml,
	"pkg/operator/testdata/highNodeUtilizationWithNamespaces.yaml":                                      pkgOperatorTestdataHighnodeutilizationwithnamespacesYaml,
	"pkg/operator/testdata/lifecycleAndUtilizationEvictPvcPodsConfig.yaml":                              pkgOperatorTestdataLifecycleandutilizationevictpvcpodsconfigYaml,
	"pkg/operator/testdata/lifecycleAndUtilizationPodLifeTimeCustomizationConfig.yaml":                  pkgOperatorTestdataLifecycleandutilizationpodlifetimecustomizationconfigYaml,
	"pkg/operator/testdata/lifecycleAndUtilizationPodLifeTimeWithThresholdPriorityClassNameConfig.yaml": pkgOperatorTestdataLifecycleandutilizationpodlifetimewiththresholdpriorityclassnameconfigYaml,
	"pkg/operator/testdata/lifecycleAndUtilizationPodLifeTimeWithThresholdPriorityConfig.yaml":          pkgOperatorTestdataLifecycleandutilizationpodlifetimewiththresholdpriorityconfigYaml,
	"pkg/operator/testdata/longLifecycleWithLocalStorage.yaml":                                          pkgOperatorTestdataLonglifecyclewithlocalstorageYaml,
	"pkg/operator/testdata/longLifecycleWithNamespaces.yaml":                                            pkgOperatorTestdataLonglifecyclewithnamespacesYaml,
	"pkg/operator/testdata/lowNodeUtilizationHighConfig.yaml":                                           pkgOperatorTestdataLownodeutilizationhighconfigYaml,
	"pkg/operator/testdata/lowNodeUtilizationLowConfig.yaml":                                            pkgOperatorTestdataLownodeutilizationlowconfigYaml,
	"pkg/operator/testdata/lowNodeUtilizationMediumConfig.yaml":                                         pkgOperatorTestdataLownodeutilizationmediumconfigYaml,
	"pkg/operator/testdata/softTopologyAndDuplicates.yaml":                                              pkgOperatorTestdataSofttopologyandduplicatesYaml,
	"pkg/operator/testdata/topologyAndDuplicates.yaml":                                                  pkgOperatorTestdataTopologyandduplicatesYaml,
}

// AssetDir returns the file names below a certain
// directory embedded in the file by go-bindata.
// For example if you run go-bindata on data/... and data contains the
// following hierarchy:
//
//	data/
//	  foo.txt
//	  img/
//	    a.png
//	    b.png
//
// then AssetDir("data") would return []string{"foo.txt", "img"}
// AssetDir("data/img") would return []string{"a.png", "b.png"}
// AssetDir("foo.txt") and AssetDir("notexist") would return an error
// AssetDir("") will return []string{"data"}.
func AssetDir(name string) ([]string, error) {
	node := _bintree
	if len(name) != 0 {
		cannonicalName := strings.Replace(name, "\\", "/", -1)
		pathList := strings.Split(cannonicalName, "/")
		for _, p := range pathList {
			node = node.Children[p]
			if node == nil {
				return nil, fmt.Errorf("Asset %s not found", name)
			}
		}
	}
	if node.Func != nil {
		return nil, fmt.Errorf("Asset %s not found", name)
	}
	rv := make([]string, 0, len(node.Children))
	for childName := range node.Children {
		rv = append(rv, childName)
	}
	return rv, nil
}

type bintree struct {
	Func     func() (*asset, error)
	Children map[string]*bintree
}

var _bintree = &bintree{nil, map[string]*bintree{
	"pkg": {nil, map[string]*bintree{
		"operator": {nil, map[string]*bintree{
			"testdata": {nil, map[string]*bintree{
				"affinityAndTaintsWithNamespaces.yaml":                                        {pkgOperatorTestdataAffinityandtaintswithnamespacesYaml, map[string]*bintree{}},
				"highNodeUtilization.yaml":                                                    {pkgOperatorTestdataHighnodeutilizationYaml, map[string]*bintree{}},
				"highNodeUtilizationMinimal.yaml":                                             {pkgOperatorTestdataHighnodeutilizationminimalYaml, map[string]*bintree{}},
				"highNodeUtilizationModerate.yaml":                                            {pkgOperatorTestdataHighnodeutilizationmoderateYaml, map[string]*bintree{}},
				"highNodeUtilizationWithNamespaces.yaml":                                      {pkgOperatorTestdataHighnodeutilizationwithnamespacesYaml, map[string]*bintree{}},
				"lifecycleAndUtilizationEvictPvcPodsConfig.yaml":                              {pkgOperatorTestdataLifecycleandutilizationevictpvcpodsconfigYaml, map[string]*bintree{}},
				"lifecycleAndUtilizationPodLifeTimeCustomizationConfig.yaml":                  {pkgOperatorTestdataLifecycleandutilizationpodlifetimecustomizationconfigYaml, map[string]*bintree{}},
				"lifecycleAndUtilizationPodLifeTimeWithThresholdPriorityClassNameConfig.yaml": {pkgOperatorTestdataLifecycleandutilizationpodlifetimewiththresholdpriorityclassnameconfigYaml, map[string]*bintree{}},
				"lifecycleAndUtilizationPodLifeTimeWithThresholdPriorityConfig.yaml":          {pkgOperatorTestdataLifecycleandutilizationpodlifetimewiththresholdpriorityconfigYaml, map[string]*bintree{}},
				"longLifecycleWithLocalStorage.yaml":                                          {pkgOperatorTestdataLonglifecyclewithlocalstorageYaml, map[string]*bintree{}},
				"longLifecycleWithNamespaces.yaml":                                            {pkgOperatorTestdataLonglifecyclewithnamespacesYaml, map[string]*bintree{}},
				"lowNodeUtilizationHighConfig.yaml":                                           {pkgOperatorTestdataLownodeutilizationhighconfigYaml, map[string]*bintree{}},
				"lowNodeUtilizationLowConfig.yaml":                                            {pkgOperatorTestdataLownodeutilizationlowconfigYaml, map[string]*bintree{}},
				"lowNodeUtilizationMediumConfig.yaml":                                         {pkgOperatorTestdataLownodeutilizationmediumconfigYaml, map[string]*bintree{}},
				"softTopologyAndDuplicates.yaml":                                              {pkgOperatorTestdataSofttopologyandduplicatesYaml, map[string]*bintree{}},
				"topologyAndDuplicates.yaml":                                                  {pkgOperatorTestdataTopologyandduplicatesYaml, map[string]*bintree{}},
			}},
		}},
	}},
}}

// RestoreAsset restores an asset under the given directory
func RestoreAsset(dir, name string) error {
	data, err := Asset(name)
	if err != nil {
		return err
	}
	info, err := AssetInfo(name)
	if err != nil {
		return err
	}
	err = os.MkdirAll(_filePath(dir, filepath.Dir(name)), os.FileMode(0755))
	if err != nil {
		return err
	}
	err = ioutil.WriteFile(_filePath(dir, name), data, info.Mode())
	if err != nil {
		return err
	}
	err = os.Chtimes(_filePath(dir, name), info.ModTime(), info.ModTime())
	if err != nil {
		return err
	}
	return nil
}

// RestoreAssets restores an asset under the given directory recursively
func RestoreAssets(dir, name string) error {
	children, err := AssetDir(name)
	// File
	if err != nil {
		return RestoreAsset(dir, name)
	}
	// Dir
	for _, child := range children {
		err = RestoreAssets(dir, filepath.Join(name, child))
		if err != nil {
			return err
		}
	}
	return nil
}

func _filePath(dir, name string) string {
	cannonicalName := strings.Replace(name, "\\", "/", -1)
	return filepath.Join(append([]string{dir}, strings.Split(cannonicalName, "/")...)...)
}
