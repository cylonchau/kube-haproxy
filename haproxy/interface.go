package haproxy

import (
	"github.com/haproxytech/models"
)

type HaproxyInterface interface {
	EnsureHaproxy() bool
	// EnsureFrontendBind checks if address is bound to the frontend and, if not, binds it.  If the frontend is already bound, return true.
	EnsureFrontendBind(bindName, frontendName string) bool
	// EnsureBackend checks if backend is already created return false.
	EnsureBackend(name string) bool
	// EnsureFrontend checks if frontend is already created return false.
	EnsureFrontend(name string) bool
	// UnbindFrontend unbind address from the frontend
	unbindFrontend(name string) (exist bool, err error)
	// DeleteBackend deletes the given Backend by name.
	DeleteBackend(name string) bool
	// DeleteBackend deletes the given frontend by name.
	DeleteFrontend(name string) bool

	DeleteServerFromBackend(serverName, backendName string) (bool, error)

	checkPortIsAvailable(protocol string, port int) (status bool)

	getVersion() int

	GetServer(backendName, srvName string) Server

	GetServers(backendName string) Servers

	AddFrontend(payload *models.Frontend) (bool, error)
	AddBind(payload *models.Bind, frontendName string) (bool, error)
	AddBackend(payload *models.Backend) (bool, error)
	AddServerToBackend(payload *Server, backendName string) (bool, error)
}
