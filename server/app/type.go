package app

const (
	ROUNDROBIN = iota
	STATICRR
	LEASTCONN
	FIRST
	SOURCE
	URI
	URL_PARAM
	HDR
	RANDOM
	RDPCOOKIE
)

func Algorithm(a int) string {
	algorithmArray := []string{"roundrobin", "static-rr", "leastconn", "first ", "source", "uri", "url_param", "hdr", "random", "rdp-cookie"}
	if a > len(algorithmArray) {
		return algorithmArray[0]
	}
	return algorithmArray[a]
}
