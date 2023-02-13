package haproxy

import (
	"github.com/amit7itz/goset"
	"github.com/haproxytech/models"
)

const (
	BACKEND  = "/v2/services/haproxy/configuration/backends"
	FRONTEND = "/v2/services/haproxy/configuration/frontends"
	SERVER   = "/v2/services/haproxy/configuration/servers"
	VERSION  = "/v2/services/haproxy/configuration/version"
	BIND     = "/v2/services/haproxy/configuration/binds"
)

type ObjSet struct {
	Servers   *goset.Set[*models.Server]
	Backends  *goset.Set[*models.Backend]
	Frontends *goset.Set[*models.Frontend]
	Binds     *goset.Set[*models.Bind]
}
