# kube-haproxy

kube-haproxy 是使用haproxy替换kubernetes集群中kube-proxy IPVS/IPTables功能的

# 工作模式

kube-haproxy 可以工作为三种模式：

- **Local**: 于kube-proxy相似, 但是在部署时需要以 in-cluster 方式部署在kubentes集群中，haproxy将以sidecar模式工作在每个node节点上，而所有经由service的流量都通过127.0.0.1转发
- **only fetch**: kube-haproxy 是为外部LB提供的一个controller，这将允许用户将外部流量引入到集群内，而不使用原生的service功能，但 apiserver service仍然使用原生service(iptables/ipvs)
- **replacement kube-proxy**: kube-haproxy可以部署在集群内/外，作为控制平面提供service服务，所有service流量（包含apiserver service）都经由 外/内 补haproxy

> Notes: kube-haproxy的所有模式都不支持ingress于NodePort功能了，因为这以及替代了这两个功能

# 如何部署

```bash
./hack/mod.sh {your kubernetes version without v }
# ./hack/mod.sh 1.19.10
go build server/haproxy.go
```

# 如何运行

- local模式只能工作与集群内
- only fetch 只作为外部ingress使用
- replacement kube-proxy：这种模式下需要自行修改kube-apiserver的service controller cidr方式

# kube-haproxy参数

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