[Go kit](https://gokit.io/) is a popular Go microservices framework. I've found it quite interesting but lakcing clear and detailed examples that a learner can follow. The official hello-world tutorial [stringsvc](https://gokit.io/examples/stringsvc.html) really confused me when I tried to implement [stringsvc3](https://github.com/go-kit/kit/tree/master/examples/stringsvc3). After some strugles, I realised that all this example wanted to do was to "simulate" an [API gateway](https://microservices.io/patterns/apigateway.html), which was kinda impractical in the real world. Therefore, I combined stringsvc3 with [apigateway](https://github.com/go-kit/kit/tree/master/examples/apigateway) to create a more practical microservices application. I hope this article may help you if you want to write a useful demo with Go kit.

> In this article, I will focus on the implementation of API gateway rather than basic concepts in Go kit such as endpoint, transport or service.

# API Gateway Based On Service Discovery

[Service discovery](https://www.nginx.com/blog/service-discovery-in-a-microservices-architecture/) is an essential compoenent of a microservices architecture. In a nutshell, we don't need to bother choosing which instance of a service to use because the service discovery system will do this under the hood. 

In this example, I will use [Consul](https://www.consul.io/) to implement a very simple client-side service discovery system. 

# Service Registration

Firstly, download the source code of stringsvc3, remove the file `proxying.go` and delete the relevant code in `main.go`.

```go
// We don't need this anymore.
// svc = proxyingMiddleware(context.Background(), *proxy, logger)(svc)
```

Then, we can register the stringsvc using Go kit's sd package.

```go
package main

import (
    ....

    "github.com/go-kit/kit/sd/consul"
    "github.com/hashicorp/consul/api"
)

func main() {
    ...

	// Build consul client and register services.
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
    
    ...
}
```

It's pretty simple to register services in Consul, the service registry. You can do some additional configurations like some tags for the service. Further reading: [Go consul API's Godoc](https://godoc.org/github.com/hashicorp/consul/api).

I am gonna deploy this demo using Docker Compose later so right now I want to build a Docker image of stringsvc (I have converted this app into a Go modules project in order to manage dependencies more conveniently).

```Dockerfile
FROM golang:latest as builder

ENV GO111MODULE=on

ENV GOPROXY=https://goproxy.cn,direct

RUN mkdir /app

WORKDIR /app

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -o out

FROM alpine:latest

RUN mkdir /app

WORKDIR /app

COPY --from=builder /app/out .

CMD ["./out"]
```

# Service Discovery Client - API Gateway

Create a new Go modules project called `stringclient` that will act as the service discovery client i.e. the API gateway. In `main.go` file, we first need to build a consul instancer that yields instances for a service, which has been registered in Consul:

```go
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
    instancer := consul.NewInstancer(client, logger, serviceName, []string{}, true)
```

I think the last two parameters of `consul.NewInstancer` might be confusing (at least I was confused in the very first place). The 4th paramter, `tags`, indicates only those services with these tags will be returned. This parameter is useful only when you tag your services when you register them. Otherwise, just pass an empty slice of string. As for the last parameter `passingOnly`, only instances where both the service and any proxy are healthy will be returned if it is `true`. 

At last, we just need to create some endpoints for stringsvc and run the client. 

```go
	// uppercase endpoint
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
	countEndPointer := sd.NewEndpointer(instancer, serviceFactoryBuilder(countPath, "POST", encodeRequest, decodeResponseFuncBuilder(countResponse{})), logger)
	countEndPoint := lb.Retry(3, 3*time.Second, lb.NewRoundRobin(countEndPointer))
	http.Handle(countPath, httptransport.NewServer(
		countEndPoint,
		decodeRequestFuncBuilder(countRequest{}),
		encodeResponse,
    ))
    
	logger.Log("err", http.ListenAndServe(":8080", nil))
```

In the above code, `sd.NewEndpointer` creates an endpointer that subscibes to the instancer. The instancer can retrieve healthy instances from Consul. What the endpointer do is to fetching an instance from the "instance pool" created by the instancer and use a factory function to convert it to a client endpoint. In this case, I define a factory builder function to ocnstruct factory function based on some parameters like relative path, HTTP method, etc.

```go
func serviceFactoryBuilder(path string, method string, enc httptransport.EncodeRequestFunc, dec httptransport.DecodeResponseFunc) sd.Factory {
    // instance (host:port) is the location of an instance.
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
```

Of course, build the client program into a Docker image using the same Dockerfile in the last section.

# Deployment And Test

Like I said before, I will use Docker Compose to deploy this demo. 

```yaml
version: "3.7"

services:
  consul:
    image: consul
    command: agent -server -bootstrap -ui -client=0.0.0.0
    ports:
      - 8500:8500
      - 8600:8600/udp
    networks: 
      - gokit
  stringsvc1:
    image: stringsvc
    depends_on: 
      - consul
    ports:
      - 8001
    networks:
      - gokit
  stringsvc2:
    image: stringsvc
    depends_on: 
      - consul
    ports:
      - 8002
    networks:
      - gokit
  stringsvc3:
    image: stringsvc
    depends_on: 
      - consul
    ports:
      - 8003
    networks:
      - gokit
  stringclient:
    image: stringclient
    depends_on: 
      - consul
    ports:
      - 8080:8080
    networks:
      - gokit

networks:
  gokit:
```

I create 3 instances of stringsvc called `stringsvc1`, `stringsvc2` and `stringsvc3` respectively. I also create a client that will be the API gateway. After everything is ready, we can test the client:

```bash
$ curl -d '{"s": "foo"}' http://localhost:8080/stringsvc/uppercase
{"v":"FOO"}

$ curl -d '{"s": "foo"}' http://localhost:8080/stringsvc/count
{"v":"3"}
```

Great! It works! There is still one more thing I would like to show you before ending this article:

```bash
$ for s in foo bar baz ; do curl -d"{\"s\":\"$s\"}" localhost:8080/stringsvc/uppercase ; done
{"v":"FOO"}
{"v":"BAR"}
{"v":"BAZ"}
```

If we have a look at the logs, we will find something like this:

```bash
stringsvc2_1    | listen=:8002 caller=logging.go:22 method=uppercase input=foo output=FOO err=null took=629ns
stringsvc3_1    | listen=:8003 caller=logging.go:22 method=uppercase input=bar output=BAR err=null took=967ns
stringsvc1_1    | listen=:8001 caller=logging.go:22 method=uppercase input=baz output=BAZ err=null took=646ns
```

3 instances of the same services were called one by one and this was because we used round-robin load balancing policy when we created the endpoint.

```go
uppercaseEndpoint := lb.Retry(3, 3*time.Second, lb.NewRoundRobin(uppercaseEndpointer))
```