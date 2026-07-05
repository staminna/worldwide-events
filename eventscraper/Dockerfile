# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/eventscraper ./cmd/eventscraper

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -H app \
    && mkdir -p /data \
    && chown app /data
WORKDIR /app
COPY --from=build /out/eventscraper /usr/local/bin/eventscraper
COPY configs/cities.yaml /app/configs/cities.yaml
USER app
ENV PORT=8080 \
    DB_PATH=/data/eventscraper.db \
    CITIES_PATH=/app/configs/cities.yaml
EXPOSE 8080
VOLUME /data
ENTRYPOINT ["eventscraper"]
CMD ["serve"]
