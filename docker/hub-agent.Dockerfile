FROM golang:1.19.1-alpine as build

VOLUME ["/gocache"]

WORKDIR /app

ENV CGO_ENABLED=0 GOOS=linux
ENV GOPROXY=https://goproxy.cn,direct
ENV GOCACHE=/gocache

COPY go.mod go.sum ./
COPY cmd cmd
COPY pkg pkg

RUN go mod download

RUN go build -v -o /hub-agent ./cmd/hub-agent/main.go

FROM alpine/git:v2.30.2

# https://wiki.alpinelinux.org/wiki/Setting_the_timezone
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories && \
    apk add tzdata && \
    cp /usr/share/zoneinfo/Asia/Shanghai /etc/localtime && \
    echo Asia/Shanghai  > /etc/timezone && \
    apk del tzdata

WORKDIR /app

COPY --from=build /hub-agent .

ENTRYPOINT ["/app/hub-agent"]
