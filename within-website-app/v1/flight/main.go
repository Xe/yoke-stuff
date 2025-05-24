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
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/utils/ptr"

	v1 "github.com/Xe/yoke-stuff/within-website-app/v1"
	"github.com/yokecd/yoke/pkg/flight/wasi/k8s"

	onepasswordv1 "github.com/1Password/onepassword-operator/api/v1"
	onionv1alpha2 "github.com/bugfest/tor-controller/apis/tor/v1alpha2"
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
	result = append(result, createServiceAccount(app))

	if app.Spec.Ingress != nil && app.Spec.Ingress.Enabled {
		slog.Info("creating ingress for", "app", app.Name)
		ing, err := createIngress(app)
		if err != nil {
			return fmt.Errorf("failed to create ingress: %w", err)
		}
		result = append(result, ing)
	}

	if app.Spec.Onion != nil && app.Spec.Onion.Enabled {
		slog.Info("creating onion service for", "app", app.Name)
		result = append(result, createOnion(app))
	}

	if app.Spec.Storage != nil && app.Spec.Storage.Enabled {
		slog.Info("creating storage for", "app", app.Name)
		result = append(result, createStorage(app))
	}

	if app.Spec.Role != nil {
		slog.Info("creating role for", "app", app.Name)
		result = append(result, createRole(app))
		result = append(result, createRoleBinding(app))
	}

	// Create our resources (Deployment and Service) and encode them back out via Stdout.
	return json.NewEncoder(os.Stdout).Encode(result)
}

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
					ServiceAccountName: backend.Name,
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

	if backend.Spec.Env != nil {
		result.Spec.Template.Spec.Containers[0].Env = append(result.Spec.Template.Spec.Containers[0].Env, backend.Spec.Env...)
	}

	// if backend.Spec.Resources != nil {
	// 	for i := range result.Spec.Template.Spec.Containers {
	// 		result.Spec.Template.Spec.Containers[i].Resources = *backend.Spec.Resources
	// 	}
	// }

	for _, imagePullSecret := range backend.Spec.ImagePullSecrets {
		result.Spec.Template.Spec.ImagePullSecrets = append(result.Spec.Template.Spec.ImagePullSecrets, corev1.LocalObjectReference{
			Name: imagePullSecret,
		})
	}

	if backend.Spec.Healthcheck != nil && backend.Spec.Healthcheck.Enabled {
		if backend.Spec.Healthcheck.Port == 0 {
			backend.Spec.Healthcheck.Port = backend.Spec.Port
		}

		switch backend.Spec.Healthcheck.Kind {
		case "http":
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
			result.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
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
		case "grpc":
			result.Spec.Template.Spec.Containers[0].LivenessProbe = &corev1.Probe{
				InitialDelaySeconds: 3,
				PeriodSeconds:       10,
				ProbeHandler: corev1.ProbeHandler{
					GRPC: &corev1.GRPCAction{
						Port: int32(backend.Spec.Healthcheck.Port),
					},
				},
			}
			result.Spec.Template.Spec.Containers[0].ReadinessProbe = &corev1.Probe{
				InitialDelaySeconds: 0,
				PeriodSeconds:       10,
				ProbeHandler: corev1.ProbeHandler{
					GRPC: &corev1.GRPCAction{
						Port: int32(backend.Spec.Healthcheck.Port),
					},
				},
			}
		}
	}

	if backend.Spec.RunAsRoot {
		for i := range result.Spec.Template.Spec.Containers {
			result.Spec.Template.Spec.Containers[i].SecurityContext = nil
		}
		result.Spec.Template.Spec.SecurityContext = nil
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
			MountPath: backend.Spec.Storage.Path,
		})
	}

	return result
}

func createService(backend v1.App) *corev1.Service {
	result := &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.Identifier(),
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        backend.Name,
			Namespace:   backend.Namespace,
			Labels:      backend.Labels,
			Annotations: map[string]string{},
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

	if backend.Spec.Ingress != nil && backend.Spec.Ingress.Enabled && backend.Spec.Ingress.Kind == "grpc" {
		maps.Copy(result.Annotations, map[string]string{
			"traefik.ingress.kubernetes.io/service.serversscheme": "h2c",
		})
	}

	return result
}

func createIngress(app v1.App) (*networkingv1.Ingress, error) {
	annotations := map[string]string{
		"cert-manager.io/cluster-issuer":           app.Spec.Ingress.ClusterIssuer,
		"nginx.ingress.kubernetes.io/ssl-redirect": "true",
	}
	maps.Copy(annotations, app.Spec.Ingress.Annotations)
	result := &networkingv1.Ingress{
		TypeMeta: metav1.TypeMeta{
			APIVersion: networkingv1.SchemeGroupVersion.Identifier(),
			Kind:       "Ingress",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        app.Name,
			Namespace:   app.Namespace,
			Labels:      app.Labels,
			Annotations: annotations,
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

	if app.Spec.Ingress.EnableCoreRules {
		result.Annotations["nginx.ingress.kubernetes.io/enable-owasp-core-rules"] = "true"
		result.Annotations["nginx.ingress.kubernetes.io/enable-modsecurity"] = "true"
		result.Annotations["nginx.ingress.kubernetes.io/modsecurity-transaction-id"] = "$request_id"
	}

	if app.Spec.Ingress.Kind == "grpc" {
		maps.Copy(result.Annotations, map[string]string{
			"nginx.ingress.kubernetes.io/backend-protocol": "GRPC",
		})
	}

	var configSnippet strings.Builder

	if app.Spec.Onion != nil && app.Spec.Onion.Enabled {
		onionSvc, err := k8s.Lookup[onionv1alpha2.OnionService](k8s.ResourceIdentifier{
			ApiVersion: onionv1alpha2.GroupVersion.Identifier(),
			Kind:       "OnionService",
			Name:       app.Name,
			Namespace:  app.Namespace,
		})
		if err == nil {
			hostname := onionSvc.Status.Hostname
			if hostname != "" {
				fmt.Fprintf(&configSnippet, "more_set_headers \"Onion-Location http://%s$request_uri;\"\n", hostname)
			}
		}
	}

	// if configSnippet.Len() > 0 {
	// 	result.Annotations["nginx.ingress.kubernetes.io/configuration-snippet"] = configSnippet.String()
	// }

	return result, nil
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

func createOnion(app v1.App) *onionv1alpha2.OnionService {
	result := &onionv1alpha2.OnionService{
		TypeMeta: metav1.TypeMeta{
			APIVersion: onionv1alpha2.GroupVersion.Identifier(),
			Kind:       "OnionService",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name,
			Namespace: app.Namespace,
			Labels:    app.Labels,
		},
		Spec: onionv1alpha2.OnionServiceSpec{
			Version: int32(3),
			Rules: []onionv1alpha2.ServiceRule{
				{
					Port: networkingv1.ServiceBackendPort{
						Name:   "http",
						Number: 80,
					},
					Backend: networkingv1.IngressBackend{
						Service: &networkingv1.IngressServiceBackend{
							Name: app.Name,
							Port: networkingv1.ServiceBackendPort{
								Name:   "http",
								Number: 80,
							},
						},
					},
				},
			},
			Template: onionv1alpha2.ServicePodTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": app.Name},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{},
				},
			},
		},
	}

	var cfg strings.Builder

	if app.Spec.Onion.Haproxy {
		fmt.Fprintln(&cfg, "HiddenServiceExportCircuitID haproxy")
	}

	if app.Spec.Onion.NonAnonymous {
		fmt.Fprintln(&cfg, "HiddenServiceNonAnonymousMode 1")
		fmt.Fprintln(&cfg, "HiddenServiceSingleHopMode 1")
	}

	if app.Spec.Onion.ProofOfWorkDefense {
		fmt.Fprintln(&cfg, "HiddenServicePoWDefensesEnabled 1")
		fmt.Fprintln(&cfg, "HiddenServicePoWQueueRate 1")
		fmt.Fprintln(&cfg, "HiddenServicePoWQueueBurst 10")
	}

	result.Spec.ExtraConfig = cfg.String()

	return result
}

func createStorage(app v1.App) *corev1.PersistentVolumeClaim {
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
			Name:      app.Name + "-storage",
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
		},
	}

	return result
}

func createRole(app v1.App) *rbacv1.Role {
	return &rbacv1.Role{
		TypeMeta: metav1.TypeMeta{
			APIVersion: rbacv1.SchemeGroupVersion.Identifier(),
			Kind:       "Role",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name,
			Namespace: app.Namespace,
			Labels:    app.Labels,
		},
		Rules: app.Spec.Role.Rules,
	}
}

func createRoleBinding(app v1.App) *rbacv1.RoleBinding {
	return &rbacv1.RoleBinding{
		TypeMeta: metav1.TypeMeta{
			APIVersion: rbacv1.SchemeGroupVersion.Identifier(),
			Kind:       "RoleBinding",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      app.Name,
			Namespace: app.Namespace,
			Labels:    app.Labels,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      "ServiceAccount",
				Name:      app.Name,
				Namespace: app.Namespace,
			},
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "Role",
			Name:     app.Name,
		},
	}
}

func createServiceAccount(app v1.App) *corev1.ServiceAccount {
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
func selector(backend v1.App) map[string]string {
	return map[string]string{"app.kubernetes.io/name": backend.Name}
}
