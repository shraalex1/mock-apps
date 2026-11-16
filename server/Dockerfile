FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod ./
COPY src/ ./src/
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mock-harness ./src/cmd/harness

FROM alpine:3.21
RUN apk add --no-cache ca-certificates wget && adduser -D -u 10001 appuser
WORKDIR /app
COPY --from=builder /out/mock-harness /app/mock-harness
USER appuser
EXPOSE 4470
ENTRYPOINT ["/app/mock-harness"]
