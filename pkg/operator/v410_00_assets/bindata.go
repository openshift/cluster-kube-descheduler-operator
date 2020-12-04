// Code generated for package v410_00_assets by go-bindata DO NOT EDIT. (@generated)
// sources:
// bindata/v4.1.0/kube-descheduler/configmap.yaml
// bindata/v4.1.0/kube-descheduler/deployment.yaml
// bindata/v4.1.0/profiles/AffinityAndTaints.yaml
// bindata/v4.1.0/profiles/LifecycleAndUtilization.yaml
// bindata/v4.1.0/profiles/TopologyAndDuplicates.yaml
package v410_00_assets

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

var _v410KubeDeschedulerConfigmapYaml = []byte(`apiVersion: v1
kind: ConfigMap
metadata:
  name: "cluster"
  namespace: "openshift-kube-descheduler-operator"
data:
  "policy.yaml": ""
`)

func v410KubeDeschedulerConfigmapYamlBytes() ([]byte, error) {
	return _v410KubeDeschedulerConfigmapYaml, nil
}

func v410KubeDeschedulerConfigmapYaml() (*asset, error) {
	bytes, err := v410KubeDeschedulerConfigmapYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "v4.1.0/kube-descheduler/configmap.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _v410KubeDeschedulerDeploymentYaml = []byte(`apiVersion: apps/v1
kind: Deployment
metadata:
  name: "descheduler"
  namespace: "openshift-kube-descheduler-operator"
  labels:
    app: "descheduler"
spec:
  replicas: 1
  selector:
    matchLabels:
      app: "descheduler"
  template:
    metadata:
      labels:
        app: "descheduler"
    spec:
      volumes:
        - name: "policy-volume"
          configMap:
            name: "descheduler"
      priorityClassName: "system-cluster-critical"
      restartPolicy: "Always"
      containers:
        - name: "openshift-descheduler"
          image: ${IMAGE}
          resources:
            limits:
              cpu: "100m"
              memory: "500Mi"
            requests:
              cpu: "100m"
              memory: "500Mi"
          command: ["/bin/descheduler"]
          args:
            - --policy-config-file=/policy-dir/policy.yaml
            - --v=2
          volumeMounts:
            - mountPath: "/policy-dir"
              name: "policy-volume"
      serviceAccountName: "openshift-descheduler"
`)

func v410KubeDeschedulerDeploymentYamlBytes() ([]byte, error) {
	return _v410KubeDeschedulerDeploymentYaml, nil
}

func v410KubeDeschedulerDeploymentYaml() (*asset, error) {
	bytes, err := v410KubeDeschedulerDeploymentYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "v4.1.0/kube-descheduler/deployment.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _v410ProfilesAffinityandtaintsYaml = []byte(`apiVersion: "descheduler/v1alpha1"
kind: "DeschedulerPolicy"
strategies:
  "RemovePodsViolatingInterPodAntiAffinity":
    enabled: true
  "RemovePodsViolatingNodeTaints":
    enabled: true
  "RemovePodsViolatingNodeAffinity":
    enabled: true
    params:
      nodeAffinityType:
      - "requiredDuringSchedulingIgnoredDuringExecution"
`)

func v410ProfilesAffinityandtaintsYamlBytes() ([]byte, error) {
	return _v410ProfilesAffinityandtaintsYaml, nil
}

func v410ProfilesAffinityandtaintsYaml() (*asset, error) {
	bytes, err := v410ProfilesAffinityandtaintsYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "v4.1.0/profiles/AffinityAndTaints.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _v410ProfilesLifecycleandutilizationYaml = []byte(`apiVersion: "descheduler/v1alpha1"
kind: "DeschedulerPolicy"
strategies:
  "PodLifeTime":
     enabled: true
     params:
       podLifeTime:
         maxPodLifeTimeSeconds: 86400 #24 hours
  "RemovePodsHavingTooManyRestarts":
     enabled: true
     params:
       podsHavingTooManyRestarts:
         podRestartThreshold: 100
         includingInitContainers: true
  "LowNodeUtilization":
     enabled: true
     params:
       nodeResourceUtilizationThresholds:
         thresholds:
           "cpu" : 20
           "memory": 20
           "pods": 20
         targetThresholds:
           "cpu" : 50
           "memory": 50
           "pods": 50
`)

func v410ProfilesLifecycleandutilizationYamlBytes() ([]byte, error) {
	return _v410ProfilesLifecycleandutilizationYaml, nil
}

func v410ProfilesLifecycleandutilizationYaml() (*asset, error) {
	bytes, err := v410ProfilesLifecycleandutilizationYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "v4.1.0/profiles/LifecycleAndUtilization.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
	a := &asset{bytes: bytes, info: info}
	return a, nil
}

var _v410ProfilesTopologyandduplicatesYaml = []byte(`apiVersion: "descheduler/v1alpha1"
kind: "DeschedulerPolicy"
strategies:
  "RemovePodsViolatingTopologySpreadConstraint":
    enabled: true
  "RemoveDuplicates":
    enabled: true
`)

func v410ProfilesTopologyandduplicatesYamlBytes() ([]byte, error) {
	return _v410ProfilesTopologyandduplicatesYaml, nil
}

func v410ProfilesTopologyandduplicatesYaml() (*asset, error) {
	bytes, err := v410ProfilesTopologyandduplicatesYamlBytes()
	if err != nil {
		return nil, err
	}

	info := bindataFileInfo{name: "v4.1.0/profiles/TopologyAndDuplicates.yaml", size: 0, mode: os.FileMode(0), modTime: time.Unix(0, 0)}
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
	"v4.1.0/kube-descheduler/configmap.yaml":       v410KubeDeschedulerConfigmapYaml,
	"v4.1.0/kube-descheduler/deployment.yaml":      v410KubeDeschedulerDeploymentYaml,
	"v4.1.0/profiles/AffinityAndTaints.yaml":       v410ProfilesAffinityandtaintsYaml,
	"v4.1.0/profiles/LifecycleAndUtilization.yaml": v410ProfilesLifecycleandutilizationYaml,
	"v4.1.0/profiles/TopologyAndDuplicates.yaml":   v410ProfilesTopologyandduplicatesYaml,
}

// AssetDir returns the file names below a certain
// directory embedded in the file by go-bindata.
// For example if you run go-bindata on data/... and data contains the
// following hierarchy:
//     data/
//       foo.txt
//       img/
//         a.png
//         b.png
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
	"v4.1.0": {nil, map[string]*bintree{
		"kube-descheduler": {nil, map[string]*bintree{
			"configmap.yaml":  {v410KubeDeschedulerConfigmapYaml, map[string]*bintree{}},
			"deployment.yaml": {v410KubeDeschedulerDeploymentYaml, map[string]*bintree{}},
		}},
		"profiles": {nil, map[string]*bintree{
			"AffinityAndTaints.yaml":       {v410ProfilesAffinityandtaintsYaml, map[string]*bintree{}},
			"LifecycleAndUtilization.yaml": {v410ProfilesLifecycleandutilizationYaml, map[string]*bintree{}},
			"TopologyAndDuplicates.yaml":   {v410ProfilesTopologyandduplicatesYaml, map[string]*bintree{}},
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
