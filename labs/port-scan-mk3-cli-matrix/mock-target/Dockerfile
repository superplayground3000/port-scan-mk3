FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 go build -o /bin/mock-target .

FROM alpine:3.20
RUN apk add --no-cache iptables && adduser -D -u 10001 app
COPY --from=build /bin/mock-target /usr/local/bin/mock-target
USER app
ENTRYPOINT ["/usr/local/bin/mock-target"]
