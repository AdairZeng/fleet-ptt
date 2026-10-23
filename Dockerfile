# syntax=docker/dockerfile:1

FROM node:22-alpine AS web-build
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.22-alpine AS go-build
WORKDIR /src/server
COPY server/go.mod ./
RUN go mod download
COPY server/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/fleet-ptt ./cmd/server

FROM alpine:3.22 AS runtime
RUN apk add --no-cache ca-certificates wget \
	&& mkdir -p /data /app/web
WORKDIR /app
COPY --from=go-build /out/fleet-ptt /app/fleet-ptt
COPY --from=web-build /src/web/dist /app/web/dist
ENV HTTP_ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/app/fleet-ptt"]
