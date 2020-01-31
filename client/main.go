package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"github.com/go-kit/kit/endpoint"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/sd"
	"github.com/go-kit/kit/sd/consul"
	"github.com/go-kit/kit/sd/lb"
	httptransport "github.com/go-kit/kit/transport/http"
	"github.com/hashicorp/consul/api"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	var(
		consulServer = os.Getenv("CONSUL_SERVER")
		listen = os.Getenv("LISTEN")
		serviceName = os.Getenv("SERVICE")
		prefix = os.Getenv("PREFIX")
	)

	logger := log.NewLogfmtLogger(os.Stderr)
	logger = log.With(logger, "ts", log.DefaultTimestampUTC)
	logger = log.With(logger, "caller", log.DefaultCaller)

	// Build instancer.
	consulConfig := api.DefaultConfig()
	if len(consulServer) > 0 {
		consulConfig.Address = consulServer
	}
	consulClient, err := api.NewClient(consulConfig)
	if err != nil {
		logger.Log("err", err)
		os.Exit(1)
	}
	client := consul.NewClient(consulClient)

	if len(serviceName) == 0 {
		logger.Log("err", errors.New("service name is empty"))
		os.Exit(1)
	}
	instancer := consul.NewInstancer(client, logger, serviceName, []string{}, true)

	// uppercase endpoint
	uppercasePath := prefix + "/uppercase"
	// Create an endpointer that subscibes to the instancer.
	uppercaseEndpointer := sd.NewEndpointer(instancer, serviceFactoryBuilder(uppercasePath, "POST", encodeRequest, decodeResponseFuncBuilder(uppercaseResponse{})), logger)
    // Use round-robin load balancing.
    // Set retry policy.
	uppercaseEndpoint := lb.Retry(3, 3*time.Second, lb.NewRoundRobin(uppercaseEndpointer))
	http.Handle(uppercasePath, httptransport.NewServer(
		uppercaseEndpoint,
		decodeRequestFuncBuilder(uppercaseRequest{}),
		encodeResponse,
	))
	// count endpoint
	countPath := prefix + "/count"
	countEndPointer := sd.NewEndpointer(instancer, serviceFactoryBuilder(countPath, "POST", encodeRequest, decodeResponseFuncBuilder(countResponse{})), logger)
	countEndPoint := lb.Retry(3, 3*time.Second, lb.NewRoundRobin(countEndPointer))
	http.Handle(countPath, httptransport.NewServer(
		countEndPoint,
		decodeRequestFuncBuilder(countRequest{}),
		encodeResponse,
	))

	if len(listen) == 0 {
		listen = ":8080"
	}
	logger.Log("err", http.ListenAndServe(listen, nil))
}

func serviceFactoryBuilder(path string, method string, enc httptransport.EncodeRequestFunc, dec httptransport.DecodeResponseFunc) sd.Factory {
	return func(instance string) (e endpoint.Endpoint, closer io.Closer, err error) {
		httpPrefix := "http://"
		if !strings.HasPrefix(instance, httpPrefix) {
			instance = httpPrefix + instance
		}

		tgt, err := url.Parse(instance)
		if err != nil {
			return nil, nil, err
		}
		tgt.Path = path

		return httptransport.NewClient(method, tgt, enc, dec).Endpoint(), nil, nil
	}
}

func encodeRequest(_ context.Context, r *http.Request, request interface{}) error {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(request); err != nil {
		return err
	}
	r.Body = ioutil.NopCloser(&buf)
	return nil
}

func encodeResponse(_ context.Context, w http.ResponseWriter, response interface{}) error {
	return json.NewEncoder(w).Encode(response)
}

func decodeRequestFuncBuilder(request interface{}) httptransport.DecodeRequestFunc {
	return func(_ context.Context, r *http.Request) (_request interface{}, err error) {
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			return nil, err
		}
		return request, nil
	}
}

func decodeResponseFuncBuilder(response interface{}) httptransport.DecodeResponseFunc {
	return func(_ context.Context, r *http.Response) (_response interface{}, err error) {
		if err := json.NewDecoder(r.Body).Decode(&response); err != nil {
			return nil, err
		}
		return response, nil
	}
}

type uppercaseRequest struct {
	S string `json:"s"`
}

type uppercaseResponse struct {
	V   string `json:"v"`
	Err string `json:"err,omitempty"`
}

type countRequest struct {
	S string `json:"s"`
}

type countResponse struct {
	V int `json:"v"`
}
