FROM golang:1.22-bookworm AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download 2>/dev/null || true
COPY . .
RUN go mod tidy && CGO_ENABLED=0 go build -o /out/trace ./cmd/trace

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*
WORKDIR /app
COPY --from=build /out/trace /app/trace
COPY web/dist /app/web/dist
ENV TRACE_WEB_DIR=/app/web/dist
ENV TRACE_OBJECT_DIR=/data/objects
EXPOSE 8080
ENTRYPOINT ["/app/trace"]
