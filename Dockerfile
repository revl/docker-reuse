FROM golang:1.15 AS build

WORKDIR /go/src/docker-reuse

COPY . .

RUN go build -v

FROM gcr.io/cloud-builders/docker:19.03.8

COPY --from=build /go/src/docker-reuse/docker-reuse /usr/local/bin/

ENV DOCKER_CLI_EXPERIMENTAL=enabled

ENTRYPOINT ["/usr/local/bin/docker-reuse"]
