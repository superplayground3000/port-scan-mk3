FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY main.go ./
RUN CGO_ENABLED=0 go build -o /bin/mock-pressure .

FROM alpine:3.20
RUN adduser -D -u 10001 app
COPY --from=build /bin/mock-pressure /usr/local/bin/mock-pressure
USER app
ENTRYPOINT ["/usr/local/bin/mock-pressure"]
