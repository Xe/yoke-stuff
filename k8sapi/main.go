package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/yokecd/yoke/pkg/flight"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
)

var (
	image    = flag.String("image", "alpine:latest", "container image")
	replicas = flag.Int("replicas", 2, "number of replicas")
)

func main() {
	flag.Parse()

	if err := run(*replicas, *image); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(replicas int, image string) error {
	var (
		release   = flight.Release()   // the first argument passed to yoke takeoff;       ie: yoke takeoff RELEASE foo
		namespace = flight.Namespace() // the value of the flag namespace during takeoff;  ie: yoke takeoff -namespace NAMESPACE ...
		labels    = map[string]string{"app": release}
	)

	resources := []flight.Resource{
		CreateDeployment(DeploymentConfig{
			Name:      release,
			Namespace: namespace,
			Labels:    labels,
			Replicas:  int32(replicas),
			Image:     image,
		}),
		CreateService(ServiceConfig{
			Name:       release,
			Namespace:  namespace,
			Labels:     labels,
			Port:       80,
			TargetPort: 80,
		}),
	}

	return json.NewEncoder(os.Stdout).Encode(resources)
}

type DeploymentConfig struct {
	Name      string
	Namespace string
	Image     string
	Labels    map[string]string
	Replicas  int32
}

func CreateDeployment(cfg DeploymentConfig) *appsv1.Deployment {
	return &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: appsv1.SchemeGroupVersion.Identifier(),
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.Name,
			Namespace: cfg.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: cfg.Labels,
			},
			Replicas: ptr.To(cfg.Replicas),
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: cfg.Labels,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    cfg.Name,
							Image:   cfg.Image,
							Command: []string{"watch", "echo", "hello world"},
						},
					},
				},
			},
		},
	}
}

type ServiceConfig struct {
	Name       string
	Namespace  string
	Labels     map[string]string
	Port       int32
	TargetPort int
}

func CreateService(cfg ServiceConfig) *corev1.Service {
	return &corev1.Service{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Service",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfg.Name,
			Namespace: cfg.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Type:     corev1.ServiceTypeClusterIP,
			Selector: cfg.Labels,
			Ports: []corev1.ServicePort{
				{
					Protocol:   corev1.ProtocolTCP,
					Port:       cfg.Port,
					TargetPort: intstr.FromInt(cfg.TargetPort),
				},
			},
		},
	}
}
