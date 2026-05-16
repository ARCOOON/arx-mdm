# syntax=docker/dockerfile:1

# --- Stage 1: React dashboard (Vite production build)
FROM node:22-bookworm AS frontend
WORKDIR /web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web ./
RUN npm run build

# --- Stage 2: Go backend + embedded dashboard + embedded agent binaries
FROM golang:1.26-bookworm AS backend
WORKDIR /src
ENV GOTOOLCHAIN=auto

COPY go.mod go.sum ./
RUN go mod download

COPY . .
COPY --from=frontend /web/dist ./internal/api/webui/dist

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o ./internal/serverinstall/arx-agent-linux ./cmd/agent \
	&& CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o ./internal/serverinstall/arx-agent-windows.exe ./cmd/agent \
	&& CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -tags embedbins,embedui -o /out/arx-server ./cmd/server

# --- Stage 3: minimal runtime image
FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata postgresql16-client
RUN mkdir -p /app
COPY --from=backend /out/arx-server /app/arx-server
EXPOSE 8080
ENTRYPOINT ["/app/arx-server"]
