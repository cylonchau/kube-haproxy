# kube-haproxy

[中文](READMECN.md)

kube-haproxy is a kube-proxy replacement solition on Kubernetes.

It features:

- High Availability HAProxy only on kubernetes in-cluster, support function failover. 
- Auto configure kubernetes service resouce
- Developed in Golang, has no additional dependency (only haproxy).

# Theory

kube-haproxy using haproxy replace IPVS/iptables mod on kubernetes service

# work mode 

- **Local**: similar kube-proxy, each node will deployment haproxy with node sidecar way. all traffic via haproxy 127.0.0.1 forward to Pod.
- **only fetch**: kube-haproxy is a only external ingress on kubernetes, if using service function on kubernetes, service traffic use naive kubernetes service, kube-haproxy service only provide a bypass internal service function.
- **replacement kube-proxy**: all traffic via haproxy to Pod, contain internal/external service.

> Notes: ingress and NodePort function not available in all modes.

# Build

```bash
./hack/mod.sh {your kubernetes version without v }
# ./hack/mod.sh 1.19.10 correct
# ./hack/mod.sh v1.19.10 mistake
go build server/haproxy.go
```

# run

- local need running in kubernetes cluster-in mode
- only fetch at will.
- replacement kube-proxy need change kube-apiserver service controller.

# argument
- --interface string                 can specify special network interface name (only local mode).
- --kube-api-burst int32             Burst to use while talking with kubernetes apiserver
- --kube-api-content-type string     Content type of requests sent to apiserver.
- --kube-api-qps float32             QPS to use while talking with kubernetes apiserver
- --kubeconfig string                Path to kubeconfig file with authorization information (the master location is
  set by the master flag).
- --min-sync-period duration         The minimum interval of how often the haproxy rules can be refreshed as endpoin
  ts and services change (e.g. '5s', '1m', '2h22m').
- --proxy-mode string                Which proxy mode to use: 'onlyfetch' or 'local' (similar kube-proxy) or without
  proxy. If blank, default only fetch. 
- --config-sync-period duration      How often configuration from the apiserver is refreshed.  Must be greater than
- --user string                      control access to frontend/backend/listen sections or to http stats by allowing
  only authenticated and authorized user.
- --passwd string                    specify current user's password. Both secure (encrypted) and insecure (unencryp
    ted) passwords can be used.
- --min-sync-period duration         The minimum interval of how often the haproxy rules can be refreshed as endpoin
ts and services change (e.g. '5s', '1m', '2h22m').
- --backend-addr string              specify haproxy address.