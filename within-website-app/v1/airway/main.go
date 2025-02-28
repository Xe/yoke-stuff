package main

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"

	apiextv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/yokecd/yoke/pkg/apis/airway/v1alpha1"
	"github.com/yokecd/yoke/pkg/openapi"

	v1 "github.com/Xe/yoke-stuff/within-website-app/v1"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	return json.NewEncoder(os.Stdout).Encode(v1alpha1.Airway{
		ObjectMeta: metav1.ObjectMeta{
			Name: "apps.x.within.website",
		},
		Spec: v1alpha1.AirwaySpec{
			WasmURLs: v1alpha1.WasmURLs{
				Flight: "https://minio.xeserv.us/mi-static/yoke/x-app/v1.wasm.gz",
			},
			Template: apiextv1.CustomResourceDefinitionSpec{
				Group: "x.within.website",
				Names: apiextv1.CustomResourceDefinitionNames{
					Plural:   "apps",
					Singular: "app",
					Kind:     "App",
				},
				Scope: apiextv1.NamespaceScoped,
				Versions: []apiextv1.CustomResourceDefinitionVersion{
					{
						Name:    "v1",
						Served:  true,
						Storage: true,
						Schema: &apiextv1.CustomResourceValidation{
							OpenAPIV3Schema: openapi.SchemaFrom(reflect.TypeFor[v1.App]()),
						},
					},
				},
			},
		},
	})
}
