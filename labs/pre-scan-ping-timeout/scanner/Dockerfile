FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/port-scan ./cmd/port-scan

FROM alpine:3.20
# iputils: real `ping` for the pre-scan reachability stage (busybox ping lacks the flags
# the scanner uses). Runs with cap NET_RAW (compose) so ping can open ICMP sockets.
RUN apk add --no-cache bash iputils
COPY --from=build /bin/port-scan /usr/local/bin/port-scan
WORKDIR /lab
