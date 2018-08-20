package apiserver

import (
	"github.com/golang/glog"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/apiserver/pkg/registry/generic"
	genericapiserver "k8s.io/apiserver/pkg/server"
	serverstorage "k8s.io/apiserver/pkg/server/storage"
	"k8s.io/client-go/informers"
)

//ControllerProviderConfig controller provider config
type ControllerProviderConfig struct {
	NewFunc         func() error
	PostStartFunc   genericapiserver.PostStartHookFunc
	PreShutdownFunc genericapiserver.PreShutdownHookFunc
}

//ExtraConfig user configure
type ExtraConfig struct {
	APIResourceConfigSource serverstorage.APIResourceConfigSource
	StorageFactory          serverstorage.StorageFactory

	//RESTStorageProviders list. will be install this api.
	RESTStorageProviders []RESTStorageProvider

	//ControllerConfig config a controller
	ControllerConfig ControllerProviderConfig
}

//Config master config
type Config struct {
	GenericConfig *genericapiserver.Config
	ExtraConfig   ExtraConfig
}

type completedConfig struct {
	GenericConfig genericapiserver.CompletedConfig
	ExtraConfig   *ExtraConfig
}

//CompletedConfig complete config
type CompletedConfig struct {
	// Embed a private pointer that cannot be instantiated outside of this package.
	*completedConfig
}

// APIServer contains state for a  cluster master/apis server.
type APIServer struct {
	GenericAPIServer *genericapiserver.GenericAPIServer
}

// Complete fills in any fields not set that are required to have valid data. It's mutating the receiver.
func (cfg *Config) Complete(informers informers.SharedInformerFactory) CompletedConfig {
	c := completedConfig{
		cfg.GenericConfig.Complete(informers),
		&cfg.ExtraConfig,
	}

	c.GenericConfig.Version = &version.Info{
		Major: "1",
		Minor: "0",
	}

	return CompletedConfig{&c}
}

// New returns a new instance of WardleServer from the given config.
func (c completedConfig) New(delegateAPIServer genericapiserver.DelegationTarget) (*APIServer, error) {
	genericServer, err := c.GenericConfig.New("cticctrl-apiserver", genericapiserver.NewEmptyDelegate())
	if err != nil {
		return nil, err
	}

	gm := &APIServer{
		GenericAPIServer: genericServer,
	}

	gm.InstallAPIs(c.ExtraConfig.APIResourceConfigSource, c.GenericConfig.RESTOptionsGetter, c.ExtraConfig.RESTStorageProviders...)

	//add user config hook
	if nil == c.ExtraConfig.ControllerConfig.NewFunc() {
		controllerName := "extra-user-controller"
		m.GenericAPIServer.AddPostStartHookOrDie(controllerName, c.ExtraConfig.ControllerConfig.PostStartFunc)
		m.GenericAPIServer.AddPreShutdownHookOrDie(controllerName, c.ExtraConfig.ControllerConfig.PreShutdownFunc)
	}

	return gm, nil
}

// RESTStorageProvider is a factory type for REST storage.
type RESTStorageProvider interface {
	GroupName() string
	NewRESTStorage(apiResourceConfigSource serverstorage.APIResourceConfigSource, restOptionsGetter generic.RESTOptionsGetter) (genericapiserver.APIGroupInfo, bool)
}

// InstallAPIs will install the APIs for the restStorageProviders if they are enabled.
func (m *APIServer) InstallAPIs(apiResourceConfigSource serverstorage.APIResourceConfigSource, restOptionsGetter generic.RESTOptionsGetter, restStorageProviders ...RESTStorageProvider) {
	apiGroupsInfo := []genericapiserver.APIGroupInfo{}

	for _, restStorageBuilder := range restStorageProviders {
		groupName := restStorageBuilder.GroupName()
		if !apiResourceConfigSource.AnyVersionForGroupEnabled(groupName) {
			glog.V(1).Infof("Skipping disabled API group %q.", groupName)
			continue
		}
		apiGroupInfo, enabled := restStorageBuilder.NewRESTStorage(apiResourceConfigSource, restOptionsGetter)
		if !enabled {
			glog.Warningf("Problem initializing API group %q, skipping.", groupName)
			continue
		}
		glog.V(1).Infof("Enabling API group %q.", groupName)

		if postHookProvider, ok := restStorageBuilder.(genericapiserver.PostStartHookProvider); ok {
			name, hook, err := postHookProvider.PostStartHook()
			if err != nil {
				glog.Fatalf("Error building PostStartHook: %v", err)
			}
			m.GenericAPIServer.AddPostStartHookOrDie(name, hook)
		}

		apiGroupsInfo = append(apiGroupsInfo, apiGroupInfo)
	}

	for i := range apiGroupsInfo {
		if err := m.GenericAPIServer.InstallAPIGroup(&apiGroupsInfo[i]); err != nil {
			glog.Fatalf("Error in registering group versions: %v", err)
		}
	}
}
