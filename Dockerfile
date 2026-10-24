# syntax=docker/dockerfile:1.7

# 1. Сборка фронтенда
FROM node:22.11.0-alpine3.20 AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

# 2. Сборка бэкенда; dist фронтенда копируется внутрь Go-модуля для go:embed
FROM golang:1.23.4-alpine3.20 AS backend
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
COPY --from=frontend /src/frontend/dist/ ./internal/adapter/http/dist/
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server

# 3. Рантайм: без dev-инструментов, непривилегированный пользователь
FROM alpine:3.20.3
RUN addgroup -S app && adduser -S -G app -u 10001 app
COPY --from=backend /out/server /usr/local/bin/server
USER app
EXPOSE 8080
ENTRYPOINT ["server"]
