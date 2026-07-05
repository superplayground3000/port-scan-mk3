# Scanner + sniffer image.
#
# IMPORTANT: this image's build context is the REPOSITORY ROOT (set in
# docker-compose.yml), so it can compile the real port-scan-mk3 binary — the
# software under test — rather than a stand-in. tcpdump captures the scan on the
# container's own eth0; tshark analyzes the pcap offline (reading a file needs
# no privilege). Only tcpdump's live capture needs CAP_NET_RAW, granted in
# docker-compose.yml.
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/port-scan ./cmd/port-scan

FROM alpine:3.20
RUN apk add --no-cache tcpdump tshark bash
COPY --from=build /bin/port-scan /usr/local/bin/port-scan
COPY labs/tcp-handshake-validation/client/capture-and-scan.sh /usr/local/bin/capture-and-scan.sh
COPY labs/tcp-handshake-validation/client/inputs /work/inputs
RUN chmod +x /usr/local/bin/capture-and-scan.sh
WORKDIR /work
ENTRYPOINT ["/usr/local/bin/capture-and-scan.sh"]
