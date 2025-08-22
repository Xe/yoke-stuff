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
	KindApp    = "Postgres"
)

// App represents a backend application with opinionated defaults.
type Postgres struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              PostgresSpec `json:"spec"`
}

type PostgresSpec struct {
	Env         []corev1.EnvVar `json:"env,omitempty" yaml:"env,omitempty"`
	Healthcheck bool            `json:"healthcheck,omitempty" yaml:"healthcheck,omitempty"`

	Storage Storage  `json:"storage,omitempty" yaml:"storage,omitempty"`
	Secrets []Secret `json:"secrets,omitempty" yaml:"secrets,omitempty"`
}

type Secret struct {
	Name     string `json:"name" yaml:"name"`
	ItemPath string `json:"itemPath" yaml:"itemPath"`
}

func (s *Secret) UnmarshalJSON(data []byte) error {
	type SecretAlt Secret
	var alt SecretAlt
	if err := json.Unmarshal(data, &alt); err != nil {
		return err
	}
	if alt.ItemPath == "" {
		return fmt.Errorf("itemPath is required")
	}
	*s = Secret(alt)
	return nil
}

type Storage struct {
	Size         string  `json:"size" yaml:"size"`
	StorageClass *string `json:"storageClass,omitempty" yaml:"storageClass,omitempty"`
}

func (s *Storage) UnmarshalJSON(data []byte) error {
	type StorageAlt Storage
	var alt StorageAlt
	if err := json.Unmarshal(data, &alt); err != nil {
		return err
	}
	if alt.Size == "" {
		return fmt.Errorf("size is required")
	}

	_, err := resource.ParseQuantity(alt.Size)
	if err != nil {
		return fmt.Errorf("invalid size: %v", err)
	}

	*s = Storage(alt)
	return nil
}

// Custom Marshalling Logic so that users do not need to explicity fill out the Kind and ApiVersion.
func (v Postgres) MarshalJSON() ([]byte, error) {
	v.Kind = KindApp
	v.APIVersion = APIVersion

	type PostgresAlt Postgres
	return json.Marshal(PostgresAlt(v))
}

// Custom Unmarshalling to raise an error if the ApiVersion or Kind does not match.
func (v *Postgres) UnmarshalJSON(data []byte) error {
	type PostgresAlt Postgres
	var alt PostgresAlt
	if err := json.Unmarshal(data, &alt); err != nil {
		return err
	}
	if alt.APIVersion != APIVersion {
		return fmt.Errorf("unexpected api version: expected %s but got %s", APIVersion, alt.APIVersion)
	}
	if alt.Kind != KindApp {
		return fmt.Errorf("unexpected kind: expected %s but got %s", KindApp, alt.Kind)
	}
	*v = Postgres(alt)
	return nil
}
