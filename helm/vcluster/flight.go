package vcluster

import (
	_ "embed"
	"fmt"

	"github.com/yokecd/yoke/pkg/helm"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

//go:embed vcluster-0.23.0.tgz
var archive []byte

// RenderChart renders the chart downloaded from https://charts.loft.sh/vcluster
// Producing version: 0.23.0
func RenderChart(release, namespace string, values map[string]any) ([]*unstructured.Unstructured, error) {
	chart, err := helm.LoadChartFromZippedArchive(archive)
	if err != nil {
		return nil, fmt.Errorf("failed to load chart from zipped archive: %w", err)
	}

	return chart.Render(release, namespace, values)
}
