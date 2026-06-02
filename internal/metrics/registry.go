package metrics

import "github.com/prometheus/client_golang/prometheus"

var registry = prometheus.NewRegistry()

func Registry() *prometheus.Registry {
	return registry
}

func RegisterCollector(collector prometheus.Collector) error {
	return registry.Register(collector)
}

func MustRegister(collectors ...prometheus.Collector) {
	registry.MustRegister(collectors...)
}

func Registerer() prometheus.Registerer {
	return tolerantRegisterer{reg: registry}
}

type tolerantRegisterer struct {
	reg *prometheus.Registry
}

func (t tolerantRegisterer) Register(c prometheus.Collector) error {
	err := t.reg.Register(c)
	if err == nil {
		return nil
	}
	if _, ok := err.(prometheus.AlreadyRegisteredError); ok {
		return nil
	}
	return err
}

func (t tolerantRegisterer) MustRegister(cs ...prometheus.Collector) {
	for _, c := range cs {
		if err := t.Register(c); err != nil {
			panic(err)
		}
	}
}

func (t tolerantRegisterer) Unregister(c prometheus.Collector) bool {
	return t.reg.Unregister(c)
}
