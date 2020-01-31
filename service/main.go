package main

import (
	"errors"
	"fmt"
	"github.com/go-kit/kit/sd/consul"
	stdprometheus "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"net/http"
	"os"
	"strconv"

	"github.com/go-kit/kit/log"
	kitprometheus "github.com/go-kit/kit/metrics/prometheus"
	httptransport "github.com/go-kit/kit/transport/http"

	"github.com/hashicorp/consul/api"
)

func main() {
	var (
		host         = os.Getenv("HOST")
		listen       = os.Getenv("LISTEN")
		prefix       = os.Getenv("PREFIX")
		consulServer = os.Getenv("CONSUL_SERVER")
		serviceName  = os.Getenv("SERVICE")
	)
	if len(host) == 0 {
		host = "localhost"
	}
	if len(listen) == 0 {
		listen = ":8080"
	}

	var logger log.Logger
	logger = log.NewLogfmtLogger(os.Stderr)
	logger = log.With(logger, "listen", listen, "caller", log.DefaultCaller)
	
	// Build consul client and register services.
	if len(serviceName) == 0 {
		logger.Log("err", errors.New("service name is empty"))
		os.Exit(1)
	}
	port, err := strconv.Atoi(listen[1:])
	if err != nil {
		logger.Log("err", err)
		os.Exit(1)
	}
	// Specify the information of an instance.
	asr := api.AgentServiceRegistration{
		// Every service instance must have an unique ID.
		ID:      fmt.Sprintf("%v%v/%v", host, listen, prefix),
		Name:    serviceName,
		// These two values are the location of an instance.
		Address: host,
		Port:    port,
	}
	consulConfig := api.DefaultConfig()
	// We can get the address of consul server from environment variale or a config file.
	if len(consulServer) > 0 {
		consulConfig.Address = consulServer
	}
	consulClient, err := api.NewClient(consulConfig)
	if err != nil {
		logger.Log("err", err)
		os.Exit(1)
	}
	sdClient := consul.NewClient(consulClient)
	registar := consul.NewRegistrar(sdClient, &asr, logger)
	registar.Register()
	// According to the official doc of Go kit, 
	// it's important to call registar.Deregister() before the program exits.
	defer registar.Deregister()

	fieldKeys := []string{"method", "error"}
	requestCount := kitprometheus.NewCounterFrom(stdprometheus.CounterOpts{
		Namespace: "my_group",
		Subsystem: "string_service",
		Name:      "request_count",
		Help:      "Number of requests received.",
	}, fieldKeys)
	requestLatency := kitprometheus.NewSummaryFrom(stdprometheus.SummaryOpts{
		Namespace: "my_group",
		Subsystem: "string_service",
		Name:      "request_latency_microseconds",
		Help:      "Total duration of requests in microseconds.",
	}, fieldKeys)
	countResult := kitprometheus.NewSummaryFrom(stdprometheus.SummaryOpts{
		Namespace: "my_group",
		Subsystem: "string_service",
		Name:      "count_result",
		Help:      "The result of each count method.",
	}, []string{})

	var svc StringService
	svc = stringService{}
	svc = loggingMiddleware(logger)(svc)
	svc = instrumentingMiddleware(requestCount, requestLatency, countResult)(svc)

	// Now run services
	uppercaseHandler := httptransport.NewServer(
		makeUppercaseEndpoint(svc),
		decodeUppercaseRequest,
		encodeResponse,
	)
	countHandler := httptransport.NewServer(
		makeCountEndpoint(svc),
		decodeCountRequest,
		encodeResponse,
	)

	http.Handle(prefix+"/uppercase", uppercaseHandler)
	http.Handle(prefix+"/count", countHandler)
	http.Handle(prefix+"/metrics", promhttp.Handler())
	logger.Log("msg", "HTTP", "addr", listen)
	logger.Log("err", http.ListenAndServe(listen, nil))
}
