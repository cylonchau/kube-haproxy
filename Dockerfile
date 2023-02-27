FROM golang:alpine AS builder
MAINTAINER cylon
WORKDIR /kube-haproxy
COPY ./ /kube-haproxy
ENV GOPROXY https://goproxy.cn,direct
RUN \
    sed -i 's/dl-cdn.alpinelinux.org/mirrors.ustc.edu.cn/g' /etc/apk/repositories && \
    apk add upx bash && \
    bash hack/build.sh kube-haproxy && \
    upx -1 _output/kube-haproxy && \
    chmod +x _output/kube-haproxy

FROM haproxytech/haproxy-debian:2.6 AS runner
WORKDIR /go/kube-haproxy
COPY --from=builder /kube-haproxy/_output/kube-haproxy .
VOLUME ["/kube-haproxy"]