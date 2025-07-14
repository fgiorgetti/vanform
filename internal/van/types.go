package van

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"reflect"

	"github.com/skupperproject/skupper/pkg/apis/skupper/v2alpha1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	yamlserializer "k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/kubernetes/scheme"
)

type Controller interface {
	Start(chan struct{}) chan struct{}
}

type ControllerConfig struct {
	WatchNamespace string
	Namespace      string
	Platform       string
	Kubeconfig     string
}

type Config struct {
	VAN    string   `json:"van"`
	URL    string   `json:"url"`
	Path   string   `json:"path"`
	Secret string   `json:"secret"`
	Zones  ZoneList `json:"zones"`
}

type Zone struct {
	Name          string   `json:"name"`
	ReachableFrom []string `json:"reachable_from"`
	EndpointHost  string   `json:"endpoint_host"`
}

func (z Zone) Reachable() bool {
	return len(z.ReachableFrom) > 0
}

type ZoneList []Zone

func (z ZoneList) Reachable() bool {
	for _, zone := range z {
		if zone.Reachable() {
			return true
		}
	}
	return false
}

func (z ZoneList) HasZone(name string) bool {
	for _, zone := range z {
		if zone.Name == name {
			return true
		}
	}
	return false
}

type ConfigLoader interface {
	LoadConfig() (*Config, *corev1.Secret, error)
}

type PlatformTokenHandler interface {
	Load() ([]*Token, error)
	Save(token *Token) error
	Generate(van *Config) ([]*Token, error)
	Delete(token *Token) error
}

type Token struct {
	SiteName   string
	SiteZone   string
	TargetZone string
	Link       *v2alpha1.Link
	Secret     *corev1.Secret
}

func (t *Token) Prepare() {
	addLabels := func(meta *v1.ObjectMeta) {
		if meta.Labels == nil {
			meta.Labels = make(map[string]string)
		}
		meta.Labels["skupper.io/auto-van"] = "true"
		meta.Labels["skupper.io/site-name"] = t.SiteName
		meta.Labels["skupper.io/site-zone"] = t.SiteZone
		meta.Labels["skupper.io/target-zone"] = t.TargetZone
	}
	cleanUp := func(meta *v1.ObjectMeta) {
		meta.ManagedFields = nil
		meta.OwnerReferences = nil
		meta.ResourceVersion = ""
		meta.UID = ""
	}
	if t.Secret != nil {
		addLabels(&t.Secret.ObjectMeta)
		cleanUp(&t.Secret.ObjectMeta)
	}
	if t.Link != nil {
		addLabels(&t.Link.ObjectMeta)
		cleanUp(&t.Link.ObjectMeta)
	}
}

func (t *Token) Marshal() ([]byte, error) {
	s := json.NewSerializerWithOptions(json.DefaultMetaFactory, scheme.Scheme, scheme.Scheme, json.SerializerOptions{Yaml: true})
	buffer := new(bytes.Buffer)
	writer := bufio.NewWriter(buffer)
	_, _ = writer.Write([]byte("---\n"))
	t.Secret.TypeMeta = v1.TypeMeta{
		Kind:       "Secret",
		APIVersion: "v1",
	}
	err := s.Encode(t.Secret, writer)
	if err != nil {
		return nil, err
	}
	_, _ = writer.Write([]byte("---\n"))
	err = s.Encode(t.Link, writer)
	if err != nil {
		return nil, err
	}
	if err = writer.Flush(); err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func (t *Token) Unmarshal(data string) error {
	var err error
	buf := bytes.NewBufferString(data)
	yaml := yamlutil.NewYAMLOrJSONDecoder(buf, 1024)
	for {
		var rawObj runtime.RawExtension
		err = yaml.Decode(&rawObj)
		if err != nil {
			if err != io.EOF {
				return fmt.Errorf("error decoding file: %s", err)
			}
			break
		}
		// Decoded object from rawObject, with gvk (Group Version Kind)
		obj, gvk, err := yamlserializer.NewDecodingSerializer(unstructured.UnstructuredJSONScheme).Decode(rawObj.Raw, nil, nil)
		if err != nil {
			return err
		}
		if v2alpha1.SchemeGroupVersion == gvk.GroupVersion() {
			switch gvk.Kind {
			case "Link":
				link := &v2alpha1.Link{}
				convertTo(obj, link)
				t.Link = link
				t.SiteName = link.ObjectMeta.Labels["skupper.io/site-name"]
				t.SiteZone = link.ObjectMeta.Labels["skupper.io/site-zone"]
				t.TargetZone = link.ObjectMeta.Labels["skupper.io/target-zone"]
			}
		} else if corev1.SchemeGroupVersion == gvk.GroupVersion() {
			switch gvk.Kind {
			case "Secret":
				var secret corev1.Secret
				convertTo(obj, &secret)
				t.Secret = &secret
			}
		}
	}
	return nil
}

func (t *Token) Equals(other *Token) bool {
	if other == nil {
		return false
	}
	if t.SiteName != other.SiteName || t.SiteZone != other.SiteZone || t.TargetZone != other.TargetZone {
		return false
	}
	if !reflect.DeepEqual(t.Link.Spec, other.Link.Spec) {
		return false
	}
	if !reflect.DeepEqual(t.Secret.Data, other.Secret.Data) {
		return false
	}
	return true
}

func convertTo(obj runtime.Object, target interface{}) {
	runtime.DefaultUnstructuredConverter.FromUnstructured(obj.(runtime.Unstructured).UnstructuredContent(), target)
}
