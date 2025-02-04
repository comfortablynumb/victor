# Build the application from source
FROM golang:1.22 AS build-stage

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY cmd ./cmd
COPY internal ./internal
COPY config ./config

RUN CGO_ENABLED=0 GOOS=linux go build -o ./app ./cmd/server/main.go

# Deploy the application binary into a lean image
FROM gcr.io/distroless/base-debian11 AS release-stage

WORKDIR /app

COPY --from=build-stage /app/app /app/app
COPY --from=build-stage /app/config /app/config

EXPOSE 8080

USER nonroot:nonroot

CMD ["/app/app", "--config-path", "/app/config/config.yaml"]