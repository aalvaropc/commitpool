# syntax=docker/dockerfile:1

FROM node:22-alpine AS web
RUN corepack enable && corepack prepare pnpm@10.33.2 --activate
WORKDIR /src
COPY web/package.json web/pnpm-lock.yaml web/
RUN pnpm -C web install --frozen-lockfile
COPY web/ web/
RUN pnpm -C web install --frozen-lockfile && pnpm -C web build

FROM golang:1.26.9-alpine AS go
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY migrations/ migrations/
COPY web/embed.go web/
COPY --from=web /src/web/dist web/dist
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /commitpool ./cmd/commitpool

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=go /commitpool /commitpool
USER nonroot
EXPOSE 8080
ENTRYPOINT ["/commitpool"]
