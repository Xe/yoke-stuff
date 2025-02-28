package v1

import (
	"encoding/json"
	"fmt"

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
	AutoUpdate bool   `json:"autoUpdate,omitempty" yaml:"autoUpdate,omitempty"`
	Image      string `json:"image" yaml:"image"`
	LogLevel   string `json:"logLevel,omitempty" yaml:"logLevel,omitempty"`
	Replicas   int32  `json:"replicas,omitempty" yaml:"replicas,omitempty"`
	Port       int    `json:"port,omitempty" yaml:"port,omitempty"`

	Healthcheck *Healthcheck `json:"healthcheck,omitempty" yaml:"healthcheck,omitempty"`
	Ingress     *Ingress     `json:"ingress,omitempty" yaml:"ingress,omitempty"`

	Secrets []Secret `json:"secrets,omitempty" yaml:"secrets,omitempty"`
}

type Healthcheck struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Path    string `json:"path,omitempty" yaml:"path,omitempty"`
	Port    int    `json:"port,omitempty" yaml:"port,omitempty"`
}

type Ingress struct {
	Enabled       bool   `json:"enabled" yaml:"enabled"`
	Host          string `json:"host" yaml:"host"`
	ClusterIssuer string `json:"clusterIssuer,omitempty" yaml:"clusterIssuer,omitempty"`
	ClassName     string `json:"className,omitempty" yaml:"className,omitempty"`
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
	return nil
}
