package main

import (
	"embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"

	externaldns "github.com/Xe/yoke-stuff/helm/external-dns"
	"github.com/yokecd/yoke/pkg/flight"
	"k8s.io/apimachinery/pkg/util/yaml"

	acmev1 "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanagermetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type Config struct {
	ACME        *ACME               `json:"acme"`
	ExternalDNS *externaldns.Values `json:"externalDNS"`
	ExternalIP  IP                  `json:"externalIP"`
}

type IP struct {
	IPv4 *string `json:"ipv4,omitempty"`
	IPv6 *string `json:"ipv6,omitempty"`
}

func (ip IP) Valid() error {
	var errs []error
	if ip.IPv4 == nil && ip.IPv6 == nil {
		errs = append(errs, fmt.Errorf("ipv4 or ipv6 is required"))
	}
	if len(errs) > 0 {
		return fmt.Errorf("ip is invalid: %v", errors.Join(errs...))
	}

	return nil
}

func (c Config) Valid() error {
	var errs []error
	if c.ACME == nil {
		errs = append(errs, fmt.Errorf("acme is required"))
	} else {
		if err := c.ACME.Valid(); err != nil {
			errs = append(errs, fmt.Errorf("acme is invalid: %w", err))
		}
	}
	if c.ExternalDNS == nil {
		errs = append(errs, fmt.Errorf("externalDNS is required"))
	}
	if len(c.ExternalDNS.ExtraArgs) == 0 {
		errs = append(errs, fmt.Errorf("externalDNS.extraArgs is required"))
	}
	if err := c.ExternalIP.Valid(); err != nil {
		errs = append(errs, fmt.Errorf("externalIP is invalid: %w", err))
	}
	if len(errs) > 0 {
		return fmt.Errorf("config is invalid: %v", errors.Join(errs...))
	}

	return nil
}

type ACME struct {
	Email       string                       `json:"email"`
	Directories []ACMEDirectory              `json:"directories"`
	Solvers     []acmev1.ACMEChallengeSolver `json:"solvers"`
}

func (acme ACME) Valid() error {
	var errs []error
	if acme.Email == "" {
		errs = append(errs, fmt.Errorf("email is required"))
	}
	if len(acme.Directories) == 0 {
		errs = append(errs, fmt.Errorf("directories are required"))
	}
	for _, directory := range acme.Directories {
		if err := directory.Valid(); err != nil {
			errs = append(errs, fmt.Errorf("directory %s is invalid: %w", directory.Name, err))
		}
	}

	if len(errs) > 0 {
		return fmt.Errorf("acme is invalid: %v", errors.Join(errs...))
	}

	return nil
}

type ACMEDirectory struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

func (ad ACMEDirectory) Valid() error {
	var errs []error
	if ad.URL == "" {
		errs = append(errs, fmt.Errorf("url is required"))
	}
	if ad.Name == "" {
		errs = append(errs, fmt.Errorf("name is required"))
	}
	if len(errs) > 0 {
		return fmt.Errorf("acme directory is invalid: %v", errors.Join(errs...))
	}

	return nil
}

//go:embed data/*.yaml
var data embed.FS

func main() {
	flag.Parse()
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	var cfg Config
	fin, err := data.Open("data/default-config.yaml")
	if err != nil {
		return fmt.Errorf("failed to open default-config.yaml: %w", err)
	}
	defer fin.Close()

	if err := yaml.NewYAMLToJSONDecoder(fin).Decode(&cfg); err != nil {
		return fmt.Errorf("failed to decode default-config.yaml: %w", err)
	}

	if err := yaml.NewYAMLToJSONDecoder(os.Stdin).Decode(&cfg); err != nil && err != io.EOF {
		return fmt.Errorf("failed to decode stdin: %w", err)
	}

	if err := cfg.Valid(); err != nil {
		return fmt.Errorf("config is invalid: %w", err)
	}

	var result []any

	result = append(result, []any{corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "tor-controller-system",
		},
	}})

	fin, err = data.Open("data/tor-controller.yaml")
	if err != nil {
		return fmt.Errorf("failed to open tor-controller.yaml: %w", err)
	}
	defer fin.Close()

	torController, err := readEveryDocument(fin)
	if err != nil {
		return fmt.Errorf("failed to read tor-controller.yaml: %w", err)
	}

	result = append(result, torController)

	result = append(result, []any{corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "cert-manager",
		},
	}})

	fin, err = data.Open("data/cert-manager.yaml")
	if err != nil {
		return fmt.Errorf("failed to open cert-manager.yaml: %w", err)
	}
	defer fin.Close()

	certManager, err := readEveryDocument(fin)
	if err != nil {
		return fmt.Errorf("failed to read cert-manager.yaml: %w", err)
	}

	result = append(result, certManager)

	var directories []any

	for _, directory := range cfg.ACME.Directories {
		directories = append(directories, makeClusterIssuer(cfg.ACME, directory))
	}

	result = append(result, directories)

	fin, err = data.Open("data/external-dns-crd.yaml")
	if err != nil {
		return fmt.Errorf("failed to open external-dns-crd.yaml: %w", err)
	}
	defer fin.Close()

	extDNSCRD, err := readEveryDocument(fin)
	if err != nil {
		return fmt.Errorf("failed to read external-dns-crd.yaml: %w", err)
	}

	result = append(result, extDNSCRD)

	result = append(result, []any{corev1.Namespace{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Namespace",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "external-dns",
		},
	}})

	for _, recordType := range []string{"A", "AAAA", "CNAME", "TXT"} {
		cfg.ExternalDNS.ExtraArgs = append(cfg.ExternalDNS.ExtraArgs, "--managed-record-types="+recordType)
	}

	if cfg.ExternalIP.IPv4 != nil {
		cfg.ExternalDNS.ExtraArgs = append(cfg.ExternalDNS.ExtraArgs, "--default-targets="+*cfg.ExternalIP.IPv4)
	}
	if cfg.ExternalIP.IPv6 != nil {
		cfg.ExternalDNS.ExtraArgs = append(cfg.ExternalDNS.ExtraArgs, "--default-targets="+*cfg.ExternalIP.IPv6)
	}

	externalDNS, err := externaldns.RenderChart(flight.Release(), "external-dns", cfg.ExternalDNS)
	if err != nil {
		return fmt.Errorf("failed to render external-dns chart: %w", err)
	}

	// Filter out PodDisruptionBudgets from externalDNS
	var filteredExternalDNS []*unstructured.Unstructured
	for _, obj := range externalDNS {
		if obj.GetKind() == "PodDisruptionBudget" {
			// Skip PodDisruptionBudgets
			continue
		}
		filteredExternalDNS = append(filteredExternalDNS, obj)
	}

	result = append(result, filteredExternalDNS)

	return json.NewEncoder(os.Stdout).Encode(result)
}

func makeClusterIssuer(acme *ACME, directory ACMEDirectory) any {
	return certmanagerv1.ClusterIssuer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: certmanagerv1.SchemeGroupVersion.Identifier(),
			Kind:       "ClusterIssuer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: directory.Name,
		},
		Spec: certmanagerv1.IssuerSpec{
			IssuerConfig: certmanagerv1.IssuerConfig{
				ACME: &acmev1.ACMEIssuer{
					Server: directory.URL,
					Email:  acme.Email,
					PrivateKey: certmanagermetav1.SecretKeySelector{
						LocalObjectReference: certmanagermetav1.LocalObjectReference{
							Name: directory.Name + "-private-key",
						},
					},
					Solvers: acme.Solvers,
				},
			},
		},
	}
}

func readEveryDocument(r io.Reader) ([]unstructured.Unstructured, error) {
	var result []unstructured.Unstructured

	dec := yaml.NewYAMLToJSONDecoder(r)
	for {
		var doc unstructured.Unstructured
		if err := dec.Decode(&doc); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}

		if doc.GetAPIVersion() == "" {
			continue
		}

		result = append(result, doc)
	}

	return result, nil
}
