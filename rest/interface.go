package rest

type RequestInterface interface {
	Get() *Request
	Post() *Request
	Delete() *Request
	Put() *Request
	Verb(verb string) *Request
}
