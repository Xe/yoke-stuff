package main

import (
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

	v1 "github.com/Xe/yoke-stuff/db/valkey/v1"

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
	var app v1.Valkey
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

	slog.Info("creating deployment and service for", "valkey", app.Name)
	slog.Info("healthcheck", "hc", app.Spec.Healthcheck)
	result = append(result, createServiceAccount(app))

	if app.Spec.Storage != nil && app.Spec.Storage.Enabled {
		slog.Info("creating storage for", "app", app.Name)
		result = append(result, createStorage(app))
	}

	// Create our resources (Deployment and Service) and encode them back out via Stdout.
	return json.NewEncoder(os.Stdout).Encode(result)
}

func createDeployment(backend v1.Valkey) *appsv1.Deployment {
	result := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.Identifier(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        backend.Name + "-valkey",
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
						FSGroup: ptr.To[int64](1000),
					},
					Volumes: []corev1.Volume{
						{
							Name: "tmp",
						},
						{
							Name: "logs",
						},
						{
							Name: "etc",
						},
					},
					ServiceAccountName: backend.Name,
					Containers: []corev1.Container{
						{
							Name:            backend.Name,
							Image:           "docker.io/bitnami/valkey:latest",
							ImagePullPolicy: corev1.PullAlways,
							SecurityContext: &corev1.SecurityContext{
								RunAsUser:                ptr.To[int64](1000),
								RunAsGroup:               ptr.To[int64](1000),
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
									ContainerPort: int32(6379),
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "tmp",
									MountPath: "/opt/bitnami/valkey/tmp",
								},
								{
									Name:      "logs",
									MountPath: "/opt/bitnami/valkey/logs",
								},
								{
									Name:      "etc",
									MountPath: "/opt/bitnami/valkey/etc",
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

	if backend.Spec.Healthcheck {
		result.Spec.Template.Spec.Containers[0].LivenessProbe = &corev1.Probe{
			InitialDelaySeconds: 3,
			PeriodSeconds:       10,
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(6379),
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

	if backend.Spec.Storage != nil && backend.Spec.Storage.Enabled {
		result.Spec.Template.Spec.Volumes = append(result.Spec.Template.Spec.Volumes, corev1.Volume{
			Name: "storage",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: backend.Name + "-storage",
				},
			},
		})

		result.Spec.Template.Spec.Containers[0].VolumeMounts = append(result.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
			Name:      "storage",
			MountPath: "/bitnami/valkey/data",
		})
	}

	return result
}

func createService(backend v1.Valkey) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.Identifier(),
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      backend.Name + "-valkey",
			Namespace: backend.Namespace,
			Labels:    backend.Labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: selector(backend),
			Type:     corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       6379,
					TargetPort: intstr.FromInt(6379),
					Name:       "valkey",
				},
			},
		},
	}
}

func createOnepasswordSecret(app v1.Valkey, sec v1.Secret) *onepasswordv1.OnePasswordItem {
	genName := fmt.Sprintf("%s-valkey-%s", app.Name, sec.Name)

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

func createStorage(app v1.Valkey) *corev1.PersistentVolumeClaim {
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
			Name:      app.Name + "-valkey-storage",
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

func createServiceAccount(app v1.Valkey) *corev1.ServiceAccount {
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
func selector(backend v1.Valkey) map[string]string {
	return map[string]string{"app.kubernetes.io/name": backend.Name}
}
