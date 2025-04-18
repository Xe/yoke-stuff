package v1

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	APIVersion = "db.x.within.website/v1"
	KindApp    = "Valkey"
)

// App represents a backend application with opinionated defaults.
type Valkey struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              ValkeySpec `json:"spec"`
}

type ValkeySpec struct {
	Env         []corev1.EnvVar `json:"env,omitempty" yaml:"env,omitempty"`
	Healthcheck bool            `json:"healthcheck,omitempty" yaml:"healthcheck,omitempty"`

	Storage *Storage `json:"storage,omitempty" yaml:"storage,omitempty"`
	Secrets []Secret `json:"secrets,omitempty" yaml:"secrets,omitempty"`
}

type Secret struct {
	Name     string `json:"name" yaml:"name"`
	ItemPath string `json:"itemPath" yaml:"itemPath"`
}

func (s *Secret) UnmarshalJSON(data []byte) error {
	type SecretAlt Secret
	if err := json.Unmarshal(data, (*SecretAlt)(s)); err != nil {
		return err
	}
	if s.ItemPath == "" {
		return fmt.Errorf("itemPath is required")
	}
	return nil
}

type Storage struct {
	Enabled      bool    `json:"enabled" yaml:"enabled"`
	Size         string  `json:"size" yaml:"size"`
	StorageClass *string `json:"storageClass,omitempty" yaml:"storageClass,omitempty"`
}

func (s *Storage) UnmarshalJSON(data []byte) error {
	type StorageAlt Storage
	if err := json.Unmarshal(data, (*StorageAlt)(s)); err != nil {
		return err
	}
	if s.Enabled && s.Size == "" {
		return fmt.Errorf("size is required when storage is enabled")
	}

	_, err := resource.ParseQuantity(s.Size)
	if err != nil {
		return fmt.Errorf("invalid size: %v", err)
	}

	return nil
}

// Custom Marshalling Logic so that users do not need to explicity fill out the Kind and ApiVersion.
func (v Valkey) MarshalJSON() ([]byte, error) {
	v.Kind = KindApp
	v.APIVersion = APIVersion

	type ValkeyAlt Valkey
	return json.Marshal(ValkeyAlt(v))
}

// Custom Unmarshalling to raise an error if the ApiVersion or Kind does not match.
func (v *Valkey) UnmarshalJSON(data []byte) error {
	type ValkeyAlt Valkey
	if err := json.Unmarshal(data, (*ValkeyAlt)(v)); err != nil {
		return err
	}
	if v.APIVersion != APIVersion {
		return fmt.Errorf("unexpected api version: expected %s but got %s", APIVersion, v.APIVersion)
	}
	if v.Kind != KindApp {
		return fmt.Errorf("unexpected kind: expected %s but got %s", KindApp, v.Kind)
	}
	return nil
}
