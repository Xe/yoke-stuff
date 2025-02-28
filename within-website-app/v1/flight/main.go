package main

import (
	"cmp"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/ptr"

	// path to the package where we defined our Backend type.
	v1 "github.com/Xe/yoke-stuff/within-website-app/v1"

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
	var app v1.App
	if err := yaml.NewYAMLToJSONDecoder(os.Stdin).Decode(&app); err != nil && err != io.EOF {
		return err
	}

	// Configure some sane defaults
	app.Spec.Port = cmp.Or(app.Spec.Port, 3000)

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

	slog.Info("creating deployment and service for", "app", app.Name)
	slog.Info("healthcheck", "hc", app.Spec.Healthcheck)
	slog.Info("app", "ingress", app.Spec.Ingress)

	if app.Spec.Ingress != nil && app.Spec.Ingress.Enabled {
		slog.Info("creating ingress for", "app", app.Name)
		result = append(result, createIngress(app))
	}

	// Create our resources (Deployment and Service) and encode them back out via Stdout.
	return json.NewEncoder(os.Stdout).Encode(result)
}

// The following functions create standard kubernetes resources from our backend resource definition.
// It utilizes the base types found in `k8s.io/api` and is essentially the same as writing the types free-hand via yaml
// except that we have strong typing, type-checking, and documentation at our finger tips. All this at the reasonable
// cost of a little more verbosity.

func createDeployment(backend v1.App) *appsv1.Deployment {
	result := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.Identifier(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        backend.Name,
			Namespace:   backend.Namespace,
			Labels:      backend.Labels,
			Annotations: map[string]string{},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &backend.Spec.Replicas,
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
					Containers: []corev1.Container{
						{
							Name:            backend.Name,
							Image:           backend.Spec.Image,
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
							Env: []corev1.EnvVar{
								{
									Name:  "PORT",
									Value: strconv.Itoa(backend.Spec.Port),
								},
								{
									Name:  "BIND",
									Value: fmt.Sprintf(":%d", backend.Spec.Port),
								},
								{
									Name: "SLOG_LEVEL",
									Value: cmp.Or(
										backend.Spec.LogLevel,
										"info",
									),
								},
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          backend.Name,
									Protocol:      corev1.ProtocolTCP,
									ContainerPort: int32(backend.Spec.Port),
								},
							},
						},
					},
				},
			},
		},
	}

	if backend.Spec.AutoUpdate {
		maps.Copy(result.Annotations, map[string]string{
			"keel.sh/policy":       "all",
			"keel.sh/trigger":      "all",
			"keel.sh/pollSchedule": "@hourly",
		})
	}

	if backend.Spec.Healthcheck != nil && backend.Spec.Healthcheck.Enabled {
		result.Spec.Template.Spec.Containers[0].LivenessProbe = &corev1.Probe{
			InitialDelaySeconds: 3,
			PeriodSeconds:       10,
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: backend.Spec.Healthcheck.Path,
					Port: intstr.FromInt(backend.Spec.Healthcheck.Port),
					HTTPHeaders: []corev1.HTTPHeader{
						{
							Name:  "X-Kubernetes",
							Value: "is kinda okay",
						},
					},
				},
			},
		}
	}

	for _, sec := range backend.Spec.Secrets {
		name := fmt.Sprintf("%s-%s", backend.Name, sec.Name)

		if sec.Environment {
			result.Spec.Template.Spec.Containers[0].EnvFrom = append(result.Spec.Template.Spec.Containers[0].EnvFrom, corev1.EnvFromSource{
				SecretRef: &corev1.SecretEnvSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: name},
				},
			})
		}

		if sec.Folder {
			result.Spec.Template.Spec.Volumes = append(result.Spec.Template.Spec.Volumes, corev1.Volume{
				Name: sec.Name,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: name,
					},
				},
			})

			result.Spec.Template.Spec.Containers[0].VolumeMounts = append(result.Spec.Template.Spec.Containers[0].VolumeMounts, corev1.VolumeMount{
				Name:      name,
				MountPath: fmt.Sprintf("/run/secrets/%s", sec.Name),
			})
		}
	}

	return result
}

func createService(backend v1.App) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.Identifier(),
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      backend.Name,
			Namespace: backend.Namespace,
			Labels:    backend.Labels,
		},
		Spec: corev1.ServiceSpec{
			Selector: selector(backend),
			Type:     corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       80,
					TargetPort: intstr.FromInt(backend.Spec.Port),
					Name:       "http",
				},
			},
		},
	}
}

func createIngress(app v1.App) *networkingv1.Ingress {
	result := &networkingv1.Ingress{
		TypeMeta: metav1.TypeMeta{
			APIVersion: networkingv1.SchemeGroupVersion.Identifier(),
			Kind:       "Ingress",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name,
			Namespace: app.Namespace,
			Labels:    app.Labels,
			Annotations: map[string]string{
				"cert-manager.io/cluster-issuer": app.Spec.Ingress.ClusterIssuer,
			},
		},
		Spec: networkingv1.IngressSpec{
			IngressClassName: ptr.To(app.Spec.Ingress.ClassName),
			TLS: []networkingv1.IngressTLS{
				{
					Hosts:      []string{app.Spec.Ingress.Host},
					SecretName: mkTLSSecretName(app),
				},
			},
			Rules: []networkingv1.IngressRule{
				{
					Host: app.Spec.Ingress.Host,
					IngressRuleValue: networkingv1.IngressRuleValue{
						HTTP: &networkingv1.HTTPIngressRuleValue{
							Paths: []networkingv1.HTTPIngressPath{
								{
									PathType: ptr.To(networkingv1.PathTypePrefix),
									Path:     "/",
									Backend: networkingv1.IngressBackend{
										Service: &networkingv1.IngressServiceBackend{
											Name: app.Name,
											Port: networkingv1.ServiceBackendPort{
												Name: "http",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	return result
}

func mkTLSSecretName(app v1.App) string {
	return fmt.Sprintf("%s-public-tls", strings.ReplaceAll(app.Spec.Ingress.Host, ".", "-"))
}

func createOnepasswordSecret(app v1.App, sec v1.Secret) *onepasswordv1.OnePasswordItem {
	genName := fmt.Sprintf("%s-%s", app.Name, sec.Name)

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

// Our selector for our backend application. Independent from the regular labels passed in the backend spec.
func selector(backend v1.App) map[string]string {
	return map[string]string{"app.kubernetes.io/name": backend.Name}
}
