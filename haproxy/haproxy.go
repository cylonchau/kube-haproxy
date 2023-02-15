package haproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	restclient "github.com/cylonchau/gorest"
	"github.com/haproxytech/models"
	"k8s.io/klog/v2"
)

var _ HaproxyInterface = &HaproxyHandle{}

type HaproxyInfo struct {
	Backend  models.Backend
	Bind     models.Bind
	Frontend models.Frontend
}

type HaproxyHandle struct {
	localAddr string
	request   *restclient.Request
	mode      string
	mu        sync.Mutex
}

func (h *HaproxyHandle) EnsureHaproxy() bool {
	return true
}

func (h *HaproxyHandle) EnsureFrontendBind(bindName, frontendName string) bool {
	// parent_type is frontend fix format
	// parent_name is frontend name
	// return all bind fields of frontend
	url := fmt.Sprintf("%s/%s?parent_type=frontend&parent_name=%s", BIND, url.QueryEscape(bindName), url.QueryEscape(frontendName))

	resp := h.request.Path(url).Get().Do(context.TODO())
	if resp.Err != nil {
		klog.Error(resp.Err)
		return false
	}
	bindList := map[string][]models.Bind{}
	json.Unmarshal(resp.Body, &bindList)
	if len(bindList["data"]) > 0 {
		klog.Errorf("frontend %s has been exist bind [%s].", bindName, frontendName)
		return false
	}
	return true
}

// ensure
func (h *HaproxyHandle) EnsureBackend(backendName string) bool {
	url := fmt.Sprintf("%s/%s", BACKEND, url.QueryEscape(backendName))
	resp := h.request.Path(url).Get().Do(context.TODO())
	klog.V(4).Infof("Opeate query backend %s", backendName)
	exist, _ := handleError(&resp, backendName)
	return exist
}

func (h *HaproxyHandle) EnsureFrontend(frontendName string) bool {
	url := fmt.Sprintf("%s/%s", FRONTEND, url.QueryEscape(frontendName))
	resp := h.request.Path(url).Get().Do(context.TODO())
	klog.V(4).Infof("Opeate query backend %s", frontendName)
	exist, _ := handleError(&resp, frontendName)
	return exist
}

func (h *HaproxyHandle) EnsureServer(serverName, backendName string) bool {
	v := h.getVersion()
	url := fmt.Sprintf("%s/%s?version=%d", SERVER, url.QueryEscape(serverName), url.QueryEscape(backendName), v)
	resp := h.request.Path(url).Get().Do(context.TODO())
	klog.V(4).Infof("Opeate query server %s", serverName)
	exist, _ := handleError(&resp, backendName+":"+serverName)
	return exist
}

func (h *HaproxyHandle) ensureOneBind(bindName, frontendName string) (exist bool, err error) {
	v := h.getVersion()
	url := fmt.Sprintf("%s/%s?parent_type=frontend&frontend=%s&version=%d", BIND, url.QueryEscape(bindName), url.QueryEscape(frontendName), v)
	resp := h.request.Path(url).Get().Do(context.TODO())
	klog.V(4).Infof("Opeate get one bind %s", bindName)
	return handleError(&resp, bindName)
}

// backend series
func (h *HaproxyHandle) AddBackend(payload *models.Backend) error {
	v := h.getVersion()
	url := fmt.Sprintf("%s?version=%d", BACKEND, v)
	body, err := json.Marshal(payload)
	if err != nil {
		klog.Errorf("Failed to json convert marshal: %s\n", err)
		return err
	}
	resp := h.request.Path(url).Post().Body(body).Do(context.TODO())
	_, err = handleError(&resp, payload)
	return err
}

func (h *HaproxyHandle) GetOneBackend(backendName string) (HaproxyInfo, error) {
	url := fmt.Sprintf("%s/%s", BACKEND, url.QueryEscape(backendName))
	resp := h.request.Path(url).Get().Do(context.TODO())
	if resp.Err != nil {
		return HaproxyInfo{}, resp.Err
	}
	var backendobj map[string]models.Backend
	json.Unmarshal(resp.Body, backendobj)
	backend := backendobj["data"]
	if reflect.ValueOf(backend) == reflect.ValueOf(models.Backend{}) {
		return HaproxyInfo{}, nil
	}
	return HaproxyInfo{
		Backend: backend,
	}, nil
}

func (h *HaproxyHandle) deleteBackend(backendName string) (bool, error) {
	v := h.getVersion()
	url := fmt.Sprintf("%s/%s?version=%d", BACKEND, url.QueryEscape(backendName), v)
	resp := h.request.URL(url).Delete().Do(context.TODO())

	return handleError(&resp, backendName)
}

func (h *HaproxyHandle) ReplaceBackend(old, new *models.Backend) (bool, error) {
	v := h.getVersion()
	url := fmt.Sprintf("%s/%s?version=%d", SERVER, url.QueryEscape(old.Name), v)
	body, err := json.Marshal(new)
	if err != nil {
		klog.Errorf("Failed to json convert models.Server: %s\n", body)
		return false, err
	}
	resp := h.request.Path(url).Put().Body(body).Do(context.TODO())
	klog.V(4).Infof("Opeate replace backend [%s] to [%s]", new.Name)
	return handleError(&resp, new)
}

// frontend series
func (h *HaproxyHandle) deleteFrontend(frontendName string) (bool, error) {
	v := h.getVersion()
	url := fmt.Sprintf("%s/%s?version=%d", FRONTEND, url.QueryEscape(frontendName), v)
	resp := h.request.Path(url).Delete().Do(context.TODO())
	klog.V(4).Infof("Opeate delete frontend [%s]", frontendName)
	return handleError(&resp, frontendName)
}

func (h *HaproxyHandle) AddFrontend(payload *models.Frontend) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	v := h.getVersion()
	url := fmt.Sprintf("%s?version=%d", FRONTEND, v)
	fmt.Println(url)
	body, err := json.Marshal(payload)
	if err != nil {
		klog.Errorf("Failed to json convert marshal: %s\n", err)
		return err
	}
	resp := h.request.Path(url).Body(body).Post().Do(context.TODO())
	klog.V(4).Infof("Opeate add a frontend [%s]", payload.Name)
	_, err = handleError(&resp, payload)
	return err
}

func (h *HaproxyHandle) GetOneFrontend(frontendName string) HaproxyInfo {
	url := fmt.Sprintf("%s/%s", FRONTEND, url.QueryEscape(frontendName))
	resp := h.request.Path(url).Get().Do(context.TODO())
	if resp.Err != nil {
		return HaproxyInfo{}
	}
	var fobj map[string]models.Frontend
	json.Unmarshal(resp.Body, &fobj)
	frontend := fobj["data"]
	if reflect.ValueOf(frontend) == reflect.ValueOf(models.Frontend{}) {
		return HaproxyInfo{}
	}
	return HaproxyInfo{
		Frontend: frontend,
	}
}

func (h *HaproxyHandle) ReplaceFrontend(old, new *models.Frontend) (bool, error) {
	v := h.getVersion()
	url := fmt.Sprintf("%s/%s?version=%d", FRONTEND, url.QueryEscape(old.Name), v)
	body, err := json.Marshal(new)
	if err != nil {
		klog.Errorf("Failed to json convert marshal: %s\n", err)
		return false, err
	}
	resp := h.request.Path(url).Body(body).Post().Do(context.TODO())
	klog.V(4).Infof("Opeate replace frontend [%s] to [%s]", old.Name, new.Name)
	return handleError(&resp, new)
}

// haproxy server operations
func (h *HaproxyHandle) AddServerToBackend(payload *models.Server, backendName string) error {
	v := h.getVersion()
	url := fmt.Sprintf("%s?parent_type=backend&parent_name=%s&version=%d", SERVER, url.QueryEscape(backendName), v)
	body, err := json.Marshal(payload)
	if err != nil {
		klog.Errorf("Failed to json convert models.Server: %s\n", body)
		return err
	}
	resp := h.request.URL(url).Body(body).Post().Do(context.TODO())
	_, err = handleError(&resp, payload)
	return err
}

func (h *HaproxyHandle) deleteServerFromBackend(serverName, backendName string) (bool, error) {
	v := h.getVersion()
	url := fmt.Sprintf("%s/%s?parent_type=backend&parent_name=%s&version=%d", SERVER, url.QueryEscape(serverName), url.QueryEscape(backendName), v)
	resp := h.request.Path(url).Delete().Do(context.TODO())

	return handleError(&resp, serverName)
}

func (h *HaproxyHandle) replaceServerFromBackend(old, new *models.Server, backendName string) (bool, error) {
	v := h.getVersion()
	url := fmt.Sprintf("%s/%s?parent_type=backend&parent_name=%s&version=%d", SERVER, url.QueryEscape(old.Name), url.QueryEscape(backendName), v)
	body, err := json.Marshal(new)
	if err != nil {
		klog.Errorf("Failed to json convert models.Server: %s\n", body)
		return false, err
	}
	resp := h.request.Path(url).Put().Body(body).Do(context.TODO())
	klog.V(4).Infof("Opeate replace server [%s_%s:%d] to [%s_%s%d]", old.Name, old.Address, old.Port, new.Name, new.Address, new.Port)
	return handleError(&resp, new)
}

func (h *HaproxyHandle) GetServers(backendName string) models.Servers {
	url := fmt.Sprintf("%s?parent_type=backend&parent_name=%s", SERVER, url.QueryEscape(backendName))
	resp := h.request.Path(url).Get().Do(context.TODO())
	if resp.Err != nil {
		klog.Errorf("Failed to request: %s\n", resp.Err)
		return models.Servers{}
	}
	var servers models.Servers
	err := json.Unmarshal(resp.Body, &servers)
	if err != nil {
		klog.Errorf("Failed to json convert models.Server: %s\n", err)
		return models.Servers{}
	}
	return servers
}

func (h *HaproxyHandle) GetServer(backendName, srvName string) models.Server {
	url := fmt.Sprintf("%s/%s?parent_type=backend&parent_name=%s", SERVER, url.QueryEscape(srvName), url.QueryEscape(backendName))
	resp := h.request.Path(url).Get().Do(context.TODO())
	if resp.Err != nil {
		klog.Errorf("Failed to request: %s\n", resp.Err)
		return models.Server{}
	}
	var server models.Server
	err := json.Unmarshal(resp.Body, &server)
	if err != nil {
		klog.Errorf("Failed to json convert models.Server: %s\n", resp.Body)
		return models.Server{}
	}
	return server
}

// bind
func (h *HaproxyHandle) unbindFrontend(bindName string) (exist bool, err error) {
	v := h.getVersion()
	url := fmt.Sprintf("%s?parent_type=frontend&parent_name=%s&version=%d", BIND, url.QueryEscape(bindName), v)
	resp := h.request.Path(url).Delete().Do(context.TODO())
	klog.V(4).Infof("Opeate unbind frontend %s", bindName)
	return handleError(&resp, bindName)
}

func (h *HaproxyHandle) GetOneBind(bindName, frontName string) HaproxyInfo {
	url := fmt.Sprintf("%s/%s?parent_type=frontend&frontend=%s", BIND, url.QueryEscape(bindName), url.QueryEscape(frontName))
	resp := h.request.Path(url).Get().Do(context.TODO())
	if resp.Err != nil {
		klog.V(4).Info(resp.Err)
		return HaproxyInfo{}
	}
	var bobj map[string]models.Bind
	json.Unmarshal(resp.Body, bobj)
	bind := bobj["data"]
	if reflect.ValueOf(bind) == reflect.ValueOf(models.Bind{}) {
		return HaproxyInfo{}
	}
	return HaproxyInfo{
		Bind: bind,
	}
}

func (h *HaproxyHandle) AddBind(payload *models.Bind) error {
	v := h.getVersion()
	url := fmt.Sprintf("%s?parent_type=frontend&frontend=%s&version=%d", BIND, payload.Name, v)
	body, err := json.Marshal(payload)
	if err != nil {
		klog.Errorf("Failed to json convert marshal: %s\n", err)
		return err
	}
	resp := h.request.Path(url).Post().Body(body).Do(context.TODO())

	_, err = handleError(&resp, payload)
	return err
}

func (h *HaproxyHandle) deleteBind(bindName, frontendName string) (bool, error) {
	v := h.getVersion()
	url := fmt.Sprintf("%s/%s?parent_type=frontend&frontend=%s&version=%d", BIND, url.QueryEscape(bindName), url.QueryEscape(frontendName), v)
	resp := h.request.Path(url).Delete().Do(context.TODO())
	return handleError(&resp, bindName)
}

func (h *HaproxyHandle) replaceBind(old, new *models.Bind, frontendName string) (bool, error) {
	v := h.getVersion()
	url := fmt.Sprintf("%s/%s?parent_type=frontend&frontend=%s&version=%d", BIND, url.QueryEscape(old.Name), url.QueryEscape(frontendName), v)
	body, err := json.Marshal(new)
	if err != nil {
		klog.Errorf("Failed to json convert models.Server: %s\n", body)
		return false, err
	}
	resp := h.request.Path(url).Put().Body(body).Do(context.TODO())
	klog.V(4).Infof("Opeate replace bind [%s] to [%s]", old.Name, new.Name)
	return handleError(&resp, new)
}

//
//
//func (h *HaproxyHandle) WriteRule(info *HaproxyInfo) bool {
//	optList := goset.NewSet[interface{}]()
//	defer h.rollback(optList)
//
//	optList.Add(info.Backend)
//	err := h.AddBackend(info.Backend)
//	if err != nil {
//		return false
//	}
//
//	for _, serverItem := range info.Servers {
//		optList.Add(serverItem)
//		err := h.AddServerToBackend(serverItem, info.Backend.Name)
//		if err != nil {
//			return false
//		}
//	}
//
//	optList.Add(&info.Frontend)
//	err = h.AddFrontend(info.Frontend)
//	if err != nil {
//		return false
//	}
//
//	err = h.AddBind(info.Bind)
//	if err != nil {
//		return false
//	}
//
//	return true
//}

// common series
func (h *HaproxyHandle) checkPortIsAvailable(protocol string, port int) (status bool) {

	conn, err := net.DialTimeout(protocol, net.JoinHostPort(h.localAddr, strconv.Itoa(port)), time.Duration(checkTimeout)*time.Second)
	if err != nil {
		opErr, ok := err.(*net.OpError)
		if ok && strings.Contains(opErr.Err.Error(), "refused") {
			status = true
			return
		} else if opErr.Timeout() {
			return
		} else {
			return
		}
	}

	if conn != nil {
		defer conn.Close()
		return
	}
	return
}

func (h *HaproxyInfo) Equal(obj interface{}) bool {
	switch obj.(type) {
	case *models.Backend:
		backend := obj.(*models.Backend)
		return h.Backend.Name == backend.Name &&
			h.Backend.Mode == backend.Mode &&
			h.Backend.Balance.Algorithm == backend.Balance.Algorithm &&
			h.Backend.Httpchk == backend.Httpchk &&
			h.Backend.Forwardfor.Enabled == backend.Forwardfor.Enabled
	case *models.Frontend:
		frontend := obj.(*models.Frontend)
		return h.Frontend.Name == frontend.Name &&
			h.Frontend.Mode == frontend.Mode &&
			h.Frontend.Forwardfor == frontend.Forwardfor &&
			h.Frontend.DefaultBackend == frontend.DefaultBackend &&
			h.Frontend.Maxconn == frontend.Maxconn
	case *models.Bind:
		bind := obj.(*models.Bind)
		return h.Bind.Name == bind.Name &&
			h.Bind.Address == bind.Address &&
			h.Bind.Port == bind.Port
	}
	return false
}

//func (h *HaproxyHandle) rollback(optList *goset.Set[interface{}]) {
//	var (
//		retryNum    = 5
//		delayPeriod = rand.Intn(300) + 100
//		backendName string
//	)
//	context.Background()
//	optList.For(func(item interface{}) {
//		switch item.(type) {
//		case models.Backend:
//			var tmpItem = item.(models.Backend)
//			var circle = retryNum
//			backendName = tmpItem.Name
//			for circle > 0 {
//				_, err := h.deleteBackend(backendName)
//				if err != nil {
//					_, err = h.deleteBackend(backendName)
//					time.Sleep(time.Duration(delayPeriod))
//					circle--
//				}
//				break
//			}
//		case models.Frontend:
//			var tmpItem = item.(models.Frontend)
//			var circle = retryNum
//			for circle > 0 {
//				_, err := h.deleteFrontend(tmpItem.Name)
//				if err != nil {
//					_, err = h.deleteFrontend(tmpItem.Name)
//					time.Sleep(time.Duration(delayPeriod))
//					circle--
//				}
//				break
//			}
//		case models.Server:
//			var circle = retryNum
//			var tmpItem = item.(models.Server)
//			for circle > 0 {
//				_, err := h.deleteServerFromBackend(tmpItem.Name, backendName)
//				if err != nil {
//					_, err = h.deleteBackend(tmpItem.Name)
//					time.Sleep(time.Duration(delayPeriod))
//					circle--
//				}
//				break
//			}
//		}
//	})
//}

func (h *HaproxyHandle) getVersion() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	resp := h.request.Path(VERSION).Get().Do(context.TODO())
	if resp.Err != nil {
		return -1
	}
	version, _ := strconv.Atoi(string(resp.Body))
	return version
}

func NewHaproxyHandle(h *InitInfo) HaproxyHandle {
	req := restclient.NewDefaultRequest().BasicAuth(h.User, h.Passwd).Host(h.Host)
	var addr string
	addr = h.Host
	if addr == "" {
		addr = "127.0.0.1:5555"
	}
	var mode string
	mode = h.Mode
	if mode == "" {
		mode = OnlyFetch
	}
	a := HaproxyHandle{
		localAddr: addr,
		request:   req,
		mode:      mode,
	}
	return a
}

// useful links
// https://stackoverflow.com/questions/27410764/dial-with-a-specific-address-interface-golang
// https://stackoverflow.com/questions/22751035/golang-distinguish-ipv4-ipv6
func GetLocalAddr(dev string) (addr string, err error) {
	var (
		ief      *net.Interface
		addrs    []net.Addr
		ipv4Addr net.IP
	)
	if ief, err = net.InterfaceByName(dev); err != nil { // get interface
		return
	}
	if addrs, err = ief.Addrs(); err != nil { // get addresses
		return
	}
	for _, addr := range addrs { // get ipv4 address
		if ipv4Addr = addr.(*net.IPNet).IP.To4(); ipv4Addr != nil {
			break
		}
	}
	if ipv4Addr == nil {
		return "", errors.New(fmt.Sprintf("interface %s don't have an ipv4 address\n", dev))
	}
	return ipv4Addr.String(), nil
}

func handleError(response *restclient.Response, res interface{}) (bool, error) {

	var log200, log201, log202, log204, log400, log409, log404 string
	switch res.(type) {
	case string:
		log200 = fmt.Sprintf("The resource %s Successful operation.\n", res)
		log202 = fmt.Sprintf("The resource %s Configuration change accepted.\n", res)
		log204 = fmt.Sprintf("The resource %s deleted.\n", res)
		log404 = fmt.Sprintf("The specified resource %s was not found\n", res)
	case models.Bind:
		var payload = res.(*models.Bind)
		log200 = fmt.Sprintf("The resource %s Successful operation.\n", payload.Name)
		log202 = fmt.Sprintf("The resource %s Configuration change accepted.\n", payload.Name)
		log201 = fmt.Sprintf("The resource %s created\n", payload.Name)
		log400 = fmt.Sprintf("Bad request bind: %s\n", "bind", payload.Name)
		log404 = fmt.Sprintf("The specified resource %s was not found \n", payload.Name)
		log409 = fmt.Sprintf("The specified resource %s already exists.\n", payload.Name)
	case models.Frontend:
		var payload = res.(*models.Frontend)
		log200 = fmt.Sprintf("The resource %s Successful operation.\n", payload.Name)
		log202 = fmt.Sprintf("The resource %s Configuration change accepted.\n", payload.Name)
		log201 = fmt.Sprintf("The resource %s created\n", payload.Name)
		log400 = fmt.Sprintf("Bad request frontend: %s\n", payload.Name)
		log404 = fmt.Sprintf("The specified resource was not found %s\n", payload.Name)
		log409 = fmt.Sprintf("The specified resource %s already exists.\n", payload.Name)
	case models.Backend:
		var payload = res.(models.Backend)
		log200 = fmt.Sprintf("The resource %s Successful operation.\n", payload.Name)
		log202 = fmt.Sprintf("The resource %s Configuration change accepted.\n", payload.Name)
		log201 = fmt.Sprintf("The resource %s created\n", payload.Name)
		log400 = fmt.Sprintf("Bad request backend: %s\n", payload.Name)
		log404 = fmt.Sprintf("The specified resource was not found %s\n", payload.Name)
		log409 = fmt.Sprintf("The specified resource %s already exists.\n", payload.Name)
	case models.Server:
		var payload = res.(models.Server)
		log202 = fmt.Sprintf("The resource %s Successful operation.\n", payload.Name)
		log200 = fmt.Sprintf("The resource %s Configuration change accepted.\n", payload.Name)
		log201 = fmt.Sprintf("The resource %s created\n", payload.Name)
		log400 = fmt.Sprintf("Bad request server: %s\n", payload.Name)
		log404 = fmt.Sprintf("The specified resource was not found %s\n", payload.Name)
		log409 = fmt.Sprintf("The specified resource %s already exists.\n", payload.Name)
	}

	if response.Err == nil {
		switch response.Code {
		case http.StatusOK:
			klog.V(4).Infof(log200)
			return true, nil
		case http.StatusCreated:
			klog.V(4).Infof(log201)
			return true, nil
		case http.StatusAccepted:
			klog.V(4).Infof(log202)
			return true, nil
		case http.StatusNoContent:
			klog.V(4).Infof(log204)
			return true, fmt.Errorf(log204)
		case http.StatusBadRequest:
			klog.V(4).Infof(log400)
			return false, fmt.Errorf(log400)
		case http.StatusNotFound:
			klog.V(4).Infof(log404)
			return false, fmt.Errorf(log404)
		case http.StatusConflict:
			klog.V(4).Infof(log409)
			return false, fmt.Errorf(log409)
		default:
			klog.V(4).Infof("Unkown error, %s", string(response.Body))
			return false, fmt.Errorf("Unkown error, %s", string(response.Body))
		}
	}
	return false, response.Err
}
