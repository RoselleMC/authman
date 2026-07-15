FROM node:22-alpine AS web-build
WORKDIR /src/core/web
COPY core/web/package*.json ./
RUN npm ci
COPY core/web ./
RUN npm run build

FROM golang:1.25-alpine AS build
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src/authman
COPY go.mod ./
COPY go.sum ./
COPY internal ./internal
COPY limbo ./limbo
COPY core ./core
RUN go build -trimpath -ldflags="-s -w" -o /out/authman ./core/cmd/authman

FROM alpine:3.21
RUN adduser -D -H -u 10001 authman \
    && mkdir -p /var/lib/authman/ipgeo \
    && chown -R authman:authman /var/lib/authman
COPY --from=build /out/authman /usr/local/bin/authman
COPY --from=web-build /src/core/web/dist /usr/share/authman/core-web
ENV AUTHMAN_CORE_WEB_ROOT=/usr/share/authman/core-web
USER authman
EXPOSE 8080 8081
ENTRYPOINT ["/usr/local/bin/authman"]
