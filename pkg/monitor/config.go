package monitor

import (
	"os"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
)

type Config struct {
	// `yaml:"targets"`
	Targets []Target
}

type Target struct {
	Kinds    []schema.GroupKind
	Category string `yaml:"omitempty"`
	// TODO: add support for namespaces filtering
	// Namespaces []string `yaml:"omitempty"`
}

type YAMLConfig struct {
	Targets []struct {
		Category string
		Kinds    []string
		// Namespaces []string
	}
}

func ReadConfig(mapper meta.RESTMapper, path string) (Config, error) {
	var yamlCfg YAMLConfig
	var cfg Config
	b, err := os.ReadFile(path)

	if err != nil {
		return cfg, err
	}

	err = yaml.Unmarshal(b, &yamlCfg)
	if err != nil {
		return cfg, err
	}

	for _, t := range yamlCfg.Targets {
		var kinds []schema.GroupKind
		for _, k := range t.Kinds {
			kind, err := parseKind(mapper, k)
			if err != nil {
				klog.ErrorS(err, "Failed to parse kind", "kind", k)
				continue
			}
			kinds = append(kinds, kind)
		}
		cfg.Targets = append(cfg.Targets, Target{
			Category: t.Category,
			Kinds:    kinds,
			// Namespaces: t.Namespaces,
		})
	}

	return cfg, nil
}

func parseKind(mapper meta.RESTMapper, s string) (schema.GroupKind, error) {
	gr := schema.ParseGroupResource(s)
	gvk, err := mapper.KindFor(gr.WithVersion(""))
	if err != nil {
		return schema.GroupKind{}, err
	}
	return gvk.GroupKind(), nil
}
