FROM golang:1.26.1-bookworm AS build
ENV CGO_ENABLED=0 GOWORK=off GOMAXPROCS=2 GOMEMLIMIT=512MiB
WORKDIR /src
COPY go.mod go.sum ./
COPY relaykit/go.mod relaykit/go.sum ./relaykit/
RUN go mod download
COPY . .
RUN go test -p 2 -c -o /model.test ./model
RUN go build -p 2 -o /ren2hub-migrate ./cmd/ren2hub-migrate

FROM debian:bookworm-slim
COPY --from=build /model.test /model.test
COPY --from=build /ren2hub-migrate /ren2hub-migrate
WORKDIR /tmp
ENTRYPOINT ["/model.test"]
