package v1

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	APIVersion = "x.within.website/v1"
	KindApp    = "App"
)

// App represents a backend application with opinionated defaults.
type App struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              AppSpec `json:"spec"`
}

// Our Backend Specification
type AppSpec struct {
	AutoUpdate       bool            `json:"autoUpdate,omitempty" yaml:"autoUpdate,omitempty"`
	Image            string          `json:"image" yaml:"image"`
	ImagePullSecrets []string        `json:"imagePullSecrets,omitempty" yaml:"imagePullSecrets,omitempty"`
	LogLevel         string          `json:"logLevel,omitempty" yaml:"logLevel,omitempty"`
	Replicas         int32           `json:"replicas,omitempty" yaml:"replicas,omitempty"`
	Port             int             `json:"port,omitempty" yaml:"port,omitempty"`
	RunAsRoot        bool            `json:"runAsRoot,omitempty" yaml:"runAsRoot,omitempty"`
	Env              []corev1.EnvVar `json:"env,omitempty" yaml:"env,omitempty"`

	// Resources *corev1.ResourceRequirements `json:"resources,omitempty" yaml:"resources,omitempty"`

	Healthcheck *Healthcheck `json:"healthcheck,omitempty" yaml:"healthcheck,omitempty"`
	Ingress     *Ingress     `json:"ingress,omitempty" yaml:"ingress,omitempty"`
	Onion       *Onion       `json:"onion,omitempty" yaml:"onion,omitempty"`
	Storage     *Storage     `json:"storage,omitempty" yaml:"storage,omitempty"`
	Role        *Role        `json:"role,omitempty" yaml:"role,omitempty"`
	Anubis      *Anubis      `json:"anubis,omitempty" yaml:"anubis,omitempty"`

	Secrets []Secret `json:"secrets,omitempty" yaml:"secrets,omitempty"`
}

type Healthcheck struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Path    string `json:"path,omitempty" yaml:"path,omitempty"`
	Port    int    `json:"port,omitempty" yaml:"port,omitempty"`
}

func (h *Healthcheck) UnmarshalJSON(data []byte) error {
	type HealthcheckAlt Healthcheck
	if err := json.Unmarshal(data, (*HealthcheckAlt)(h)); err != nil {
		return err
	}
	if h.Enabled && h.Path == "" {
		h.Path = "/"
	}
	return nil
}

type Ingress struct {
	Enabled         bool              `json:"enabled" yaml:"enabled"`
	Kind            string            `json:"kind" yaml:"kind"`
	Host            string            `json:"host" yaml:"host"`
	ClusterIssuer   string            `json:"clusterIssuer,omitempty" yaml:"clusterIssuer,omitempty"`
	ClassName       string            `json:"className,omitempty" yaml:"className,omitempty"`
	EnableCoreRules bool              `json:"enableCoreRules,omitempty" yaml:"enableCoreRules,omitempty"`
	Annotations     map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty"`
}

func (i *Ingress) UnmarshalJSON(data []byte) error {
	type IngressAlt Ingress
	if err := json.Unmarshal(data, (*IngressAlt)(i)); err != nil {
		return err
	}
	if i.Enabled && i.Host == "" {
		return fmt.Errorf("host is required when ingress is enabled")
	}
	if i.Enabled && i.ClusterIssuer == "" {
		i.ClusterIssuer = "letsencrypt-prod"
	}
	if i.Enabled && i.ClassName == "" {
		i.ClassName = "nginx"
	}
	return nil
}

type Secret struct {
	Name        string `json:"name" yaml:"name"`
	ItemPath    string `json:"itemPath" yaml:"itemPath"`
	Environment bool   `json:"environment,omitempty" yaml:"environment,omitempty"` // If true, set the contents of the secret as an environment variable.
	Folder      bool   `json:"folder,omitempty" yaml:"folder,omitempty"`           // If true, set each value in the secret as a file in a folder.
}

func (s *Secret) UnmarshalJSON(data []byte) error {
	type SecretAlt Secret
	if err := json.Unmarshal(data, (*SecretAlt)(s)); err != nil {
		return err
	}
	if s.ItemPath == "" {
		return fmt.Errorf("itemPath is required")
	}
	if s.Environment && s.Folder {
		return fmt.Errorf("cannot set environment and folder at the same time")
	}
	return nil
}

type Onion struct {
	Enabled            bool `json:"enabled" yaml:"enabled"`
	NonAnonymous       bool `json:"nonAnonymous,omitempty" yaml:"nonAnonymous,omitempty"`
	Haproxy            bool `json:"haproxy,omitempty" yaml:"haproxy,omitempty"`
	ProofOfWorkDefense bool `json:"proofOfWorkDefense,omitempty" yaml:"proofOfWorkDefense,omitempty"`
}

func (o *Onion) UnmarshalJSON(data []byte) error {
	type OnionAlt Onion
	if err := json.Unmarshal(data, (*OnionAlt)(o)); err != nil {
		return err
	}
	return nil
}

type Storage struct {
	Enabled      bool    `json:"enabled" yaml:"enabled"`
	Path         string  `json:"path" yaml:"path"`
	Size         string  `json:"size" yaml:"size"`
	StorageClass *string `json:"storageClass,omitempty" yaml:"storageClass,omitempty"`
}

func (s *Storage) UnmarshalJSON(data []byte) error {
	type StorageAlt Storage
	if err := json.Unmarshal(data, (*StorageAlt)(s)); err != nil {
		return err
	}
	if s.Enabled && s.Path == "" {
		return fmt.Errorf("path is required when storage is enabled")
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

type Role struct {
	Enabled bool                `json:"enabled" yaml:"enabled"`
	Rules   []rbacv1.PolicyRule `json:"rules,omitempty" yaml:"rules,omitempty"`
}

type Anubis struct {
	Enabled  bool `json:"enabled" yaml:"enabled"`
	Settings struct {
		Difficulty     int  `json:"difficulty"`
		ServeRobotsTxt bool `json:"serveRobotsTXT"`
	} `json:"settings,omitempty,omitzero"`
}

// Custom Marshalling Logic so that users do not need to explicity fill out the Kind and ApiVersion.
func (app App) MarshalJSON() ([]byte, error) {
	app.Kind = KindApp
	app.APIVersion = APIVersion

	type AppAlt App
	return json.Marshal(AppAlt(app))
}

// Custom Unmarshalling to raise an error if the ApiVersion or Kind does not match.
func (app *App) UnmarshalJSON(data []byte) error {
	type AppAlt App
	if err := json.Unmarshal(data, (*AppAlt)(app)); err != nil {
		return err
	}
	if app.APIVersion != APIVersion {
		return fmt.Errorf("unexpected api version: expected %s but got %s", APIVersion, app.APIVersion)
	}
	if app.Kind != KindApp {
		return fmt.Errorf("unexpected kind: expected %s but got %s", KindApp, app.Kind)
	}
	if app.Spec.Replicas == 0 {
		app.Spec.Replicas = 1
	}
	return nil
}
