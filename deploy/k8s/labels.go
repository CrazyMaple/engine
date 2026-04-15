package k8s

import "fmt"

// StandardLabels 返回 Kubernetes 推荐的标准标签集
// 参考 https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/
func StandardLabels(appName, version, component, instance string) map[string]string {
	labels := map[string]string{
		"app.kubernetes.io/name":       appName,
		"app.kubernetes.io/managed-by": "engine",
	}
	if version != "" {
		labels["app.kubernetes.io/version"] = version
	}
	if component != "" {
		labels["app.kubernetes.io/component"] = component
	}
	if instance != "" {
		labels["app.kubernetes.io/instance"] = instance
	}
	return labels
}

// StandardAnnotations 返回 Prometheus 抓取相关的 Pod 注解
func StandardAnnotations(metricsPort int, metricsPath string) map[string]string {
	if metricsPath == "" {
		metricsPath = "/api/metrics/prometheus"
	}
	return map[string]string{
		"prometheus.io/scrape": "true",
		"prometheus.io/path":   metricsPath,
		"prometheus.io/port":   fmt.Sprintf("%d", metricsPort),
	}
}

// PodLabels 返回引擎 Pod 完整标签（标准标签 + 引擎自定义标签）
func PodLabels(appName, version, component, instance, clusterName string) map[string]string {
	labels := StandardLabels(appName, version, component, instance)
	if clusterName != "" {
		labels["engine.io/cluster"] = clusterName
	}
	return labels
}
