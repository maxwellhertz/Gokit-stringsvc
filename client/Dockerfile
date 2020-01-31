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