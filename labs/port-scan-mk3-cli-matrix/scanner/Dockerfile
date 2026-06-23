FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /bin/port-scan      ./cmd/port-scan \
 && CGO_ENABLED=0 go build -o /bin/preprocess     ./cmd/preprocess \
 && CGO_ENABLED=0 go build -o /bin/enrich-targets ./cmd/enrich-targets \
 && CGO_ENABLED=0 go build -o /bin/cidr-compare   ./cmd/cidr-compare \
 && CGO_ENABLED=0 go build -o /bin/csv-transform  ./cmd/csv-transform

FROM alpine:3.20
# bash: matrix driver. iputils: real `ping` for pre-scan reachability (busybox ping lacks the
# flags the scanner uses). Runs as root + cap NET_RAW (compose) so ping can open ICMP sockets.
RUN apk add --no-cache bash iputils
COPY --from=build /bin/port-scan /bin/preprocess /bin/enrich-targets /bin/cidr-compare /bin/csv-transform /usr/local/bin/
WORKDIR /lab
