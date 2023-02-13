/*
Copyright 2015 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package haproxy

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/amit7itz/goset"
	"github.com/haproxytech/models"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/klog/v2"
)

const (
	rsCheckDeleteInterval = 1 * time.Minute
)

// listItem stores real server information and the process time.
// If nothing special happened, real server will be delete after process time.
type srvItem struct {
	backendName string
	srv         *models.Server
}

type graceTerminateSrvList struct {
	lock sync.Mutex
	list goset.Set[srvItem]
}

// add push an new element to the rsList
func (q *graceTerminateSrvList) add(server *srvItem) bool {
	q.lock.Lock()
	defer q.lock.Unlock()
	if !q.list.Contains(*server) {
		return false
	}

	klog.V(5).Infof("Adding server %v to graceful delete rsList", server.srv.Address)
	q.list.Add(*server)
	return true
}

// remove remove an element from the rsList
func (q *graceTerminateSrvList) remove(server *srvItem) bool {
	q.lock.Lock()
	defer q.lock.Unlock()

	if q.list.Contains(*server) {
		err := q.list.Remove(*server)
		if err != nil {
			return false
		}
		return true
	}

	return false
}

func (q *graceTerminateSrvList) flushList(handler func(srvToDelete *srvItem) (bool, error)) bool {
	success := true
	q.list.For(func(server srvItem) {
		deleted, err := handler(&server)
		if err != nil {
			klog.Errorf("Try delete rs %q err: %v", server.srv.Name, err)
			success = false
		}
		if deleted {
			klog.Infof("lw: remote out of the list: %s", server.srv.Name)
			q.remove(&server)
		}
	})
	return success
}

// exist check whether the specified unique RS is in the rsList
func (q *graceTerminateSrvList) exist(server *srvItem) bool {
	q.lock.Lock()
	defer q.lock.Unlock()
	return q.list.Contains(*server)
}

// GracefulTerminationManager manage rs graceful termination information and do graceful termination work
// rsList is the rs list to graceful termination, ipvs is the ipvsinterface to do ipvs delete/update work
type GracefulTerminationManager struct {
	srvList graceTerminateSrvList
	haproxy HaproxyInterface
}

// NewGracefulTerminationManager create a gracefulTerminationManager to manage ipvs rs graceful termination work
func NewGracefulTerminationManager(haproxyInterface HaproxyInterface) *GracefulTerminationManager {
	return &GracefulTerminationManager{
		srvList: graceTerminateSrvList{
			list: *goset.NewSet[srvItem](),
		},
		haproxy: haproxyInterface,
	}
}

// InTerminationList to check whether specified unique rs name is in graceful termination list
func (m *GracefulTerminationManager) InTerminationList(srv *models.Server, backendName string) bool {
	return m.srvList.exist(&srvItem{
		srv:         srv,
		backendName: backendName,
	})
}

// GracefulDeleteRS to update rs weight to 0, and add rs to graceful terminate list
func (m *GracefulTerminationManager) GracefulDeleteSrv(srv *models.Server, backendName string) error {
	// Try to delete rs before add it to graceful delete list
	ele := &srvItem{
		srv:         srv,
		backendName: backendName,
	}
	deleted, err := m.deleteSrvFunc(ele)
	if err != nil {
		klog.Errorf("Delete server %s err: %v", srv.Address, err)
	}
	if deleted {
		return nil
	}

	klog.V(5).Infof("Adding an element to graceful delete srvList: %+v", ele)
	m.srvList.add(ele)
	return nil
}

func (m *GracefulTerminationManager) deleteSrvFunc(srvToDelete *srvItem) (bool, error) {
	klog.V(5).Infof("Trying to delete server in backend: %s", srvToDelete.srv.Address, srvToDelete.backendName)
	rss := m.haproxy.GetServer(srvToDelete.srv.Name, srvToDelete.backendName)
	if reflect.TypeOf(rss) == reflect.TypeOf(&models.Server{}) {
		return false, errors.New("server is nil")
	}

	klog.V(5).Infof("Deleting server in backend: %s", srvToDelete.srv.Address, srvToDelete.backendName)
	bool, err := m.haproxy.deleteServerFromBackend(srvToDelete.srv.Name, srvToDelete.backendName)
	if err != nil || !bool {
		return false, fmt.Errorf("Delete destination %s err: %v", srvToDelete.srv.Address, err)
	}
	return bool, nil
}

func (m *GracefulTerminationManager) tryDeleteSrv() {
	if !m.srvList.flushList(m.deleteSrvFunc) {
		klog.Errorf("Try flush graceful termination list err")
	}
}

// MoveRSOutofGracefulDeleteList to delete an rs and remove it from the rsList immediately
func (m *GracefulTerminationManager) MoveRSOutofGracefulDeleteList(srv *models.Server, backendName string) error {
	item := &srvItem{
		backendName: backendName,
		srv:         srv,
	}
	find := m.srvList.exist(item)
	if !find {
		return fmt.Errorf("failed to find server %s in backend: %s", srv.Address, backendName)
	}
	_, err := m.haproxy.deleteServerFromBackend(srv.Name, backendName)
	if err != nil {
		return err
	}
	m.srvList.remove(item)
	return nil
}

// Run start a goroutine to try to delete rs in the graceful delete rsList with an interval 1 minute
func (m *GracefulTerminationManager) Run() {
	go wait.Until(m.tryDeleteSrv, rsCheckDeleteInterval, wait.NeverStop)
}
