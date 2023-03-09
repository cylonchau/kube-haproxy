module github.com/cylonchau/kube-haproxy

go 1.18

replace k8s.io/api => k8s.io/api v0.19.11

replace k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.19.11

replace k8s.io/apimachinery => k8s.io/apimachinery v0.19.12-rc.0

replace k8s.io/apiserver => k8s.io/apiserver v0.19.11

replace k8s.io/cli-runtime => k8s.io/cli-runtime v0.19.11

replace k8s.io/client-go => k8s.io/client-go v0.19.11

replace k8s.io/cloud-provider => k8s.io/cloud-provider v0.19.11

replace k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.19.11

replace k8s.io/code-generator => k8s.io/code-generator v0.19.13-rc.0

replace k8s.io/component-base => k8s.io/component-base v0.19.11

replace k8s.io/controller-manager => k8s.io/controller-manager v0.19.17-rc.0

replace k8s.io/cri-api => k8s.io/cri-api v0.19.13-rc.0

replace k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.19.11

replace k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.19.11

replace k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.19.11

replace k8s.io/kube-proxy => k8s.io/kube-proxy v0.19.11

replace k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.19.11

replace k8s.io/kubectl => k8s.io/kubectl v0.19.11

replace k8s.io/kubelet => k8s.io/kubelet v0.19.11

replace k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.19.11

replace k8s.io/metrics => k8s.io/metrics v0.19.11

replace k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.19.11

replace k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.19.11

replace k8s.io/sample-controller => k8s.io/sample-controller v0.19.11

require (
	github.com/amit7itz/goset v1.2.1
	github.com/cylonchau/gorest v0.3.0
	github.com/haproxytech/client-native/v4 v4.1.4
	github.com/haproxytech/models v1.2.5-0.20191122125615-30d0235b81ec
	github.com/mitchellh/mapstructure v1.5.0
	github.com/pkg/errors v0.9.1
	github.com/shirou/gopsutil/v3 v3.23.1
	github.com/spf13/cobra v1.0.0
	github.com/spf13/pflag v1.0.5
	k8s.io/api v0.19.11
	k8s.io/apimachinery v0.19.11
	k8s.io/apiserver v0.19.11
	k8s.io/client-go v0.19.11
	k8s.io/component-base v0.19.11
	k8s.io/klog/v2 v2.80.1
	k8s.io/kube-proxy v0.0.0
	k8s.io/kubernetes v1.19.11
	k8s.io/utils v0.0.0-20221107191617-1a15be271d1d
)

require (
	github.com/asaskevich/govalidator v0.0.0-20210307081110-f21760c49a8d // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver v3.5.0+incompatible // indirect
	github.com/cespare/xxhash/v2 v2.1.1 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/go-logr/logr v1.2.3 // indirect
	github.com/go-ole/go-ole v1.2.6 // indirect
	github.com/go-openapi/analysis v0.21.4 // indirect
	github.com/go-openapi/errors v0.20.3 // indirect
	github.com/go-openapi/jsonpointer v0.19.5 // indirect
	github.com/go-openapi/jsonreference v0.20.0 // indirect
	github.com/go-openapi/loads v0.21.2 // indirect
	github.com/go-openapi/spec v0.20.7 // indirect
	github.com/go-openapi/strfmt v0.21.3 // indirect
	github.com/go-openapi/swag v0.22.3 // indirect
	github.com/go-openapi/validate v0.22.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20200121045136-8c9f03a8e57e // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/google/gofuzz v1.1.0 // indirect
	github.com/googleapis/gnostic v0.4.1 // indirect
	github.com/hashicorp/golang-lru v0.5.1 // indirect
	github.com/imdario/mergo v0.3.5 // indirect
	github.com/inconshreveable/mousetrap v1.0.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/lufia/plan9stats v0.0.0-20211012122336-39d0f177ccd0 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.2-0.20181231171920-c182affec369 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/oklog/ulid v1.3.1 // indirect
	github.com/power-devops/perfstat v0.0.0-20210106213030-5aafc221ea8c // indirect
	github.com/prometheus/client_golang v1.7.1 // indirect
	github.com/prometheus/client_model v0.2.0 // indirect
	github.com/prometheus/common v0.10.0 // indirect
	github.com/prometheus/procfs v0.1.3 // indirect
	github.com/tklauser/go-sysconf v0.3.11 // indirect
	github.com/tklauser/numcpus v0.6.0 // indirect
	github.com/yusufpapurcu/wmi v1.2.2 // indirect
	go.mongodb.org/mongo-driver v1.11.0 // indirect
	golang.org/x/crypto v0.0.0-20220622213112-05595931fe9d // indirect
	golang.org/x/net v0.0.0-20220127200216-cd36cc0744dd // indirect
	golang.org/x/oauth2 v0.0.0-20220223155221-ee480838109b // indirect
	golang.org/x/sys v0.5.0 // indirect
	golang.org/x/term v0.0.0-20210927222741-03fcf44c2211 // indirect
	golang.org/x/text v0.7.0 // indirect
	golang.org/x/time v0.0.0-20220210224613-90d013bbcef8 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/protobuf v1.28.1 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/cloud-provider v0.19.11 // indirect
	k8s.io/kube-openapi v0.0.0-20200805222855-6aeccd4b50c6 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.2.3 // indirect
	sigs.k8s.io/yaml v1.3.0 // indirect
)
