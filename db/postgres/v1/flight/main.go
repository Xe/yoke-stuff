package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/ptr"

	v1 "github.com/Xe/yoke-stuff/db/postgres/v1"

	"github.com/yokecd/yoke/pkg/flight/wasi/k8s"

	onepasswordv1 "github.com/1Password/onepassword-operator/api/v1"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	// When this flight is invoked, the atc will pass the JSON representation of the Backend instance to this program via standard input.
	// We can use the yaml to json decoder so that we can pass yaml definitions manually when testing for convenience.
	var app v1.Postgres
	if err := yaml.NewYAMLToJSONDecoder(os.Stdin).Decode(&app); err != nil && err != io.EOF {
		return err
	}

	// Make sure that our labels include our custom selector.
	if app.Labels == nil {
		app.Labels = map[string]string{}
	}
	maps.Copy(app.Labels, selector(app))

	var result []any

	for _, sec := range app.Spec.Secrets {
		result = append(result, createOnepasswordSecret(app, sec))
	}

	result = append(result, createDeployment(app))
	result = append(result, createService(app))

	// Create a consumer-facing Secret containing DATABASE_URL so other services
	// can consume a single well-known secret to reach this Postgres instance.
	result = append(result, createDatabaseSecret(app))

	slog.Info("creating deployment and service for", "postgres", app.Name)
	slog.Info("healthcheck", "hc", app.Spec.Healthcheck)
	result = append(result, createServiceAccount(app))

	// Storage is present when Size is set in the spec.
	if app.Spec.Storage.Size != "" {
		slog.Info("creating storage for", "app", app.Name)
		result = append(result, createStorage(app))
	}

	// Create our resources (Deployment and Service) and encode them back out via Stdout.
	return json.NewEncoder(os.Stdout).Encode(result)
}

func createDeployment(backend v1.Postgres) *appsv1.Deployment {
	result := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.Identifier(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        backend.Name + "-postgres",
			Namespace:   backend.Namespace,
			Labels:      backend.Labels,
			Annotations: map[string]string{},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &[]int32{1}[0],
			Strategy: appsv1.DeploymentStrategy{
				Type: appsv1.RollingUpdateDeploymentStrategyType,
			},
			Selector: &metav1.LabelSelector{MatchLabels: selector(backend)},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: backend.Labels},
				Spec: corev1.PodSpec{
					SecurityContext: &corev1.PodSecurityContext{
						FSGroup: ptr.To[int64](70),
					},
					Volumes: []corev1.Volume{
						{
							Name: "data",
						},
					},
					ServiceAccountName: backend.Name,
					Containers: []corev1.Container{
						{
							Name:            "postgres",
							Image:           "docker.io/postgres:16",
							ImagePullPolicy: corev1.PullAlways,
							SecurityContext: &corev1.SecurityContext{
								RunAsUser:                ptr.To[int64](70),
								RunAsGroup:               ptr.To[int64](70),
								RunAsNonRoot:             ptr.To(true),
								AllowPrivilegeEscalation: ptr.To(false),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{"ALL"},
								},
								SeccompProfile: &corev1.SeccompProfile{
									Type: corev1.SeccompProfileTypeRuntimeDefault,
								},
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          backend.Name,
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: int32(5432),
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "data",
									MountPath: "/var/lib/postgresql/data",
								},
							},
							Env: []corev1.EnvVar{
								{
									Name:  "POSTGRES_USER",
									Value: "postgres",
								},
								{
									Name:  "PGDATA",
									Value: "/var/lib/postgresql/data/pgdata",
								},
							},
						},
					},
				},
			},
		},
	}

	if backend.Spec.Env != nil {
		result.Spec.Template.Spec.Containers[0].Env = append(result.Spec.Template.Spec.Containers[0].Env, backend.Spec.Env...)
	}

	// Expose generated DB credentials from the conventionally-named secret
	secretName := backend.Name + "-database"
	result.Spec.Template.Spec.Containers[0].Env = append(result.Spec.Template.Spec.Containers[0].Env,
		corev1.EnvVar{
			Name: "POSTGRES_PASSWORD",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
					Key:                  "POSTGRES_PASSWORD",
					Optional:             ptr.To(false),
				},
			},
		},
		corev1.EnvVar{
			Name: "DATABASE_URL",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{Name: secretName},
					Key:                  "DATABASE_URL",
					Optional:             ptr.To(false),
				},
			},
		},
	)

	if backend.Spec.Healthcheck {
		result.Spec.Template.Spec.Containers[0].LivenessProbe = &corev1.Probe{
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(5432),
				},
			},
		}

		result.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
			InitialDelaySeconds: 5,
			PeriodSeconds:       10,
			ProbeHandler: corev1.ProbeHandler{
				Exec: &corev1.ExecAction{
					Command: []string{"pg_isready", "-U", "postgres"},
				},
			},
		}
	}

	for _, sec := range backend.Spec.Secrets {
		name := fmt.Sprintf("%s-%s", backend.Name, sec.Name)

		result.Spec.Template.Spec.Containers[0].EnvFrom = append(result.Spec.Template.Spec.Containers[0].EnvFrom, corev1.EnvFromSource{
			SecretRef: &corev1.SecretEnvSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: name},
			},
		})
	}

	// Back the existing "data" volume with the PVC so the container's
	// existing volumeMount (name: "data", mountPath: /var/lib/postgresql/data)
	// is satisfied by the PersistentVolumeClaim. This avoids creating a
	// second VolumeMount with the same mountPath which would cause a
	// duplicate-mountPath error when applying the Deployment.
	if len(result.Spec.Template.Spec.Volumes) > 0 && result.Spec.Template.Spec.Volumes[0].Name == "data" {
		result.Spec.Template.Spec.Volumes[0].VolumeSource = corev1.VolumeSource{
			PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
				ClaimName: backend.Name + "-postgres-storage",
			},
		}
	} else {
		// Fallback: append a data volume if the initial one isn't present.
		result.Spec.Template.Spec.Volumes = append(result.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: "data",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: backend.Name + "-postgres-storage",
				},
			},
		})
	}
	// Do not append another VolumeMount; the container already mounts "data".

	return result
}

func createService(backend v1.Postgres) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.Identifier(),
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      backend.Name + "-postgres",
			Namespace: backend.Namespace,
			Labels:    backend.Labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: selector(backend),
			Type:     corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       5432,
					TargetPort: intstr.FromInt(5432),
					Name:       "postgres",
				},
			},
		},
	}
}

func createOnepasswordSecret(app v1.Postgres, sec v1.Secret) *onepasswordv1.OnePasswordItem {
	genName := fmt.Sprintf("%s-postgres-%s", app.Name, sec.Name)

	result := &onepasswordv1.OnePasswordItem{
		TypeMeta: metav1.TypeMeta{
			APIVersion: onepasswordv1.GroupVersion.Identifier(),
			Kind:       "OnePasswordItem",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        genName,
			Namespace:   app.Namespace,
			Labels:      app.Labels,
			Annotations: map[string]string{},
		},
		Spec: onepasswordv1.OnePasswordItemSpec{
			ItemPath: sec.ItemPath,
		},
	}

	return result
}

func createDatabaseSecret(app v1.Postgres) *corev1.Secret {
	// Name the secret <app.Name>-database so consumers can find it by convention.
	name := app.Name + "-database"

	// Host the service DNS for cluster-internal access. Use the service created above
	// which is named <app.Name>-postgres in the same namespace.
	svcFQDN := fmt.Sprintf("%s.%s.svc", app.Name+"-postgres", app.Namespace)

	// We'll resolve/generate the password below and then compose a proper DATABASE_URL
	// that embeds the generated or existing password.
	dbURL := ""

	// Attempt to look up an existing secret and reuse its password if present.
	secretName := app.Name + "-database"
	existing, err := k8s.Lookup[corev1.Secret](k8s.ResourceIdentifier{
		ApiVersion: "v1",
		Kind:       "Secret",
		Name:       secretName,
		Namespace:  app.Namespace,
	})
	if err != nil && !k8s.IsErrNotFound(err) {
		// lookup failed in a way other than not-found; panic because the flight cannot continue reliably.
		panic(fmt.Errorf("failed to lookup secret: %v", err))
	}

	password := func() string {
		if existing != nil {
			if b, ok := existing.Data["POSTGRES_PASSWORD"]; ok {
				return string(b)
			}
		}
		return RandomString()
	}()

	// Compose final DATABASE_URL using the resolved password.
	dbURL = fmt.Sprintf("postgres://%s:%s@%s:%d/%s", "postgres", password, svcFQDN, 5432, app.Name)

	result := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.Identifier(),
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: app.Namespace,
			Labels:    app.Labels,
		},
		StringData: map[string]string{
			"DATABASE_URL":      dbURL,
			"POSTGRES_PASSWORD": password,
		},
		Type: corev1.SecretTypeOpaque,
	}

	return result
}

func createStorage(app v1.Postgres) *corev1.PersistentVolumeClaim {
	size, err := resource.ParseQuantity(app.Spec.Storage.Size)
	if err != nil {
		panic(err)
	}

	result := &corev1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.Identifier(),
			Kind:       "PersistentVolumeClaim",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name + "-postgres-storage",
			Namespace: app.Namespace,
			Labels:    app.Labels,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{
				corev1.ReadWriteOnce,
			},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: size,
				},
			},
			StorageClassName: app.Spec.Storage.StorageClass,
			VolumeMode:       &[]corev1.PersistentVolumeMode{corev1.PersistentVolumeFilesystem}[0],
		},
	}

	return result
}

func createServiceAccount(app v1.Postgres) *corev1.ServiceAccount {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.Identifier(),
			Kind:       "ServiceAccount",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name,
			Namespace: app.Namespace,
			Labels:    app.Labels,
		},
		AutomountServiceAccountToken: ptr.To(true),
	}
}

// Our selector for our backend application. Independent from the regular labels passed in the backend spec.
func selector(backend v1.Postgres) map[string]string {
	return map[string]string{"app.kubernetes.io/name": backend.Name}
}

func RandomString() string {
	buf := make([]byte, 16)
	rand.Read(buf)
	return fmt.Sprintf("%x", buf)
}
