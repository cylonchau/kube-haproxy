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
	Backends  *goset.Set[*models.Backend]
	Frontends *goset.Set[*models.Frontend]
	Binds     *goset.Set[*models.Bind]
}

const (
	Local            = "local"
	OnlyFetch        = "of"
	MeshAll          = "mesh"
	processName      = "haproxy"
	defaultInterface = "eth0"
	BACKEND_PREFIX   = "BACKEND."
	FRONTEND_PREFIX  = "FRONTEND."
	BIND_PREFIX      = "BIND."
	SERVER_PREFIX    = "POD."
)

type InitInfo struct {
	Host                    string
	User, Passwd, Dev, Mode string
	IsCheckServer           bool
	ServerAlgorithm         string
}

type HaproxyInfo struct {
	Backend  models.Backend
	Bind     models.Bind
	Frontend models.Frontend
}

type Service struct {
	Name     string
	Backend  models.Backend
	Frontend models.Frontend
}

type Services []Service

var (
	checkTimeout   = int64(1)
	forwordEnable  = "enabled"
	forwordDisable = "disabled"
)

type Server struct {

	// address
	// Pattern: ^[^\s]+$
	Address string `json:"address,omitempty"`

	// allow 0rtt
	Allow0rtt bool `json:"allow_0rtt,omitempty"`

	// backup
	// Enum: [enabled disabled]
	Backup string `json:"backup,omitempty"`

	// check
	// Enum: [enabled disabled]
	Check string `json:"check,omitempty"`

	// cookie
	// Pattern: ^[^\s]+$
	Cookie string `json:"cookie,omitempty"`

	// inter
	Inter int64 `json:"inter,omitempty"`

	// maintenance
	// Enum: [enabled disabled]
	Maintenance string `json:"maintenance,omitempty"`

	// maxconn
	Maxconn int64 `json:"maxconn,omitempty"`

	// name
	// Required: true
	// Pattern: ^[^\s]+$
	Name string `json:"name"`

	// on error
	// Enum: [fastinter fail-check sudden-death mark-down]
	OnError string `json:"on-error,omitempty"`

	// on marked down
	// Enum: [shutdown-sessions]
	OnMarkedDown string `json:"on-marked-down,omitempty"`

	// on marked up
	// Enum: [shutdown-backup-sessions]
	OnMarkedUp string `json:"on-marked-up,omitempty"`

	// port
	// Maximum: 65535
	// Minimum: 0
	Port int64 `json:"port,omitempty"`

	// send proxy
	// Enum: [enabled disabled]
	SendProxy string `json:"send-proxy,omitempty"`

	// send proxy v2
	// Enum: [enabled disabled]
	SendProxyV2 string `json:"send-proxy-v2,omitempty"`

	// ssl
	// Enum: [enabled disabled]
	Ssl string `json:"ssl,omitempty"`

	// ssl cafile
	// Pattern: ^[^\s]+$
	SslCafile string `json:"ssl_cafile,omitempty"`

	// ssl certificate
	// Pattern: ^[^\s]+$
	SslCertificate string `json:"ssl_certificate,omitempty"`

	// tls tickets
	// Enum: [enabled disabled]
	TLSTickets string `json:"tls_tickets,omitempty"`

	// verify
	// Enum: [none required]
	Verify string `json:"verify,omitempty"`

	// weight
	Weight int64 `json:"weight,omitempty"`
}

type Servers []Server
