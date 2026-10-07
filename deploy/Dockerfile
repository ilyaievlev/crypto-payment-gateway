FROM golang:1.25.6-alpine AS build
ARG SERVICE
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN case "$SERVICE" in api-gateway|payment-core|chain-worker|webhook-sender|crypto-vault) ;; *) exit 1 ;; esac && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server "./$SERVICE/cmd/server"

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/server /server
USER nonroot:nonroot
ENTRYPOINT ["/server"]
