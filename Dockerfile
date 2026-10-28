# Serves the web UI on plain HTTP. It refuses to start without WEBTUIDB_PASSWORD or WEBTUIDB_TOKEN (or an explicit WEBTUIDB_INSECURE=1):
# docker run -p 127.0.0.1:7788:7788 -e WEBTUIDB_PASSWORD=... --user $(id -u) -v ~/.config/webtuidb:/home/nonroot/.config/webtuidb webtuidb
FROM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
# vite writes into ../internal/web/dist, which the Go build embeds
RUN npm run build

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/internal/web/dist ./internal/web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.Version=${VERSION}" -o /webtuidb .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /webtuidb /webtuidb
EXPOSE 7788
ENTRYPOINT ["/webtuidb","--frontend","--listen","0.0.0.0:7788","--insecure-listen"]
