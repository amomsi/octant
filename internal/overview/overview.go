package overview

import (
	"fmt"
	"github.com/heptio/developer-dash/internal/cache"
	"net/http"
	"os"
	"sync"

	"github.com/davecgh/go-spew/spew"
	"github.com/heptio/developer-dash/internal/cluster"
	"github.com/heptio/developer-dash/internal/hcli"
	"github.com/heptio/developer-dash/internal/log"
	"github.com/pkg/errors"
	"k8s.io/client-go/restmapper"
)

// ClusterOverview is an API for generating a cluster overview.
type ClusterOverview struct {
	client cluster.ClientInterface

	mu sync.Mutex

	namespace string

	logger log.Logger

	cache  cache.Cache
	stopCh chan struct{}

	generator *realGenerator
}

// NewClusterOverview creates an instance of ClusterOverview.
func NewClusterOverview(client cluster.ClientInterface, namespace string, logger log.Logger) (*ClusterOverview, error) {
	stopCh := make(chan struct{})

	var opts []cache.InformerCacheOpt

	if os.Getenv("DASH_VERBOSE_CACHE") != "" {
		ch := make(chan cache.CacheNotification)

		go func() {
			for notif := range ch {
				spew.Dump(notif)
			}
		}()

		opts = append(opts, cache.InformerCacheNotificationOpt(ch, stopCh))
	}

	if client == nil {
		return nil, errors.New("nil cluster client")
	}

	dynamicClient, err := client.DynamicClient()
	if err != nil {
		return nil, errors.Wrapf(err, "creating DynamicClient")
	}
	di, err := client.DiscoveryClient()
	if err != nil {
		return nil, errors.Wrapf(err, "creating DiscoveryClient")
	}

	groupResources, err := restmapper.GetAPIGroupResources(di)
	if err != nil {
		logger.Errorf("discovering APIGroupResources: %v", err)
		return nil, errors.Wrapf(err, "mapping APIGroupResources")
	}
	rm := restmapper.NewDiscoveryRESTMapper(groupResources)

	opts = append(opts, cache.InformerCacheLoggerOpt(logger))
	cache := cache.NewInformerCache(stopCh, dynamicClient, rm, opts...)

	var pathFilters []pathFilter
	pathFilters = append(pathFilters, rootDescriber.PathFilters(namespace)...)
	pathFilters = append(pathFilters, eventsDescriber.PathFilters(namespace)...)

	g := newGenerator(cache, pathFilters, client)

	co := &ClusterOverview{
		namespace: namespace,
		client:    client,
		logger:    logger,
		cache:     cache,
		generator: g,
		stopCh:    stopCh,
	}
	return co, nil
}

// Name returns the name for this module.
func (co *ClusterOverview) Name() string {
	return "overview"
}

// ContentPath returns the content path for overview.
func (co *ClusterOverview) ContentPath() string {
	return fmt.Sprintf("/%s", co.Name())
}

// Handler returns a handler for serving overview HTTP content.
func (co *ClusterOverview) Handler(prefix string) http.Handler {
	return newHandler(prefix, co.generator, stream, co.logger)
}

// Navigation returns navigation entries for overview.
func (co *ClusterOverview) Navigation(root string) (*hcli.Navigation, error) {
	return navigationEntries(root)
}

// SetNamespace sets the current namespace.
func (co *ClusterOverview) SetNamespace(namespace string) error {
	co.logger.With("namespace", namespace, "module", "overview").Debugf("setting namespace")
	co.namespace = namespace
	return nil
}

// Start starts overview.
func (co *ClusterOverview) Start() error {
	return nil
}

// Stop stops overview.
func (co *ClusterOverview) Stop() {
	co.mu.Lock()
	defer co.mu.Unlock()
	close(co.stopCh)
	co.stopCh = nil
}
