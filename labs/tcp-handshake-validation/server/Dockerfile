# Minimal TCP listener (the scan target). Multi-stage: static Go binary on a
# distroless nonroot runtime.
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/target .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /bin/target /usr/local/bin/target
# Health probe dials the listen port over loopback (invisible to eth0 capture).
HEALTHCHECK --interval=2s --timeout=2s --retries=15 \
  CMD ["/usr/local/bin/target", "-healthcheck", "-addr", ":9000"]
ENTRYPOINT ["/usr/local/bin/target", "-addr", ":9000"]
