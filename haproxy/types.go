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

type ModeType string

const (
	Local            = "local"
	OnlyFetch        = "of"
	MeshAll          = "mesh"
	processName      = "haproxy"
	defaultInterface = "eth0"
)

type InitInfo struct {
	Host                    string
	User, Passwd, Dev, Mode string
	IsCheckServer           bool
}

type HaproxyInfo struct {
	Backend  models.Backend
	Bind     models.Bind
	Frontend models.Frontend
}
type Services struct {
	Backend  []models.Backend
	Frontend []models.Frontend
}

var (
	checkTimeout   = int64(1)
	forwordEnable  = "enabled"
	forwordDisable = "disabled"
)
