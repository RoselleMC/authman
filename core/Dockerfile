FROM node:22-alpine AS web-build
WORKDIR /src/authman/core/web
COPY authman/core/web/package*.json ./
RUN npm ci
COPY authman/core/web ./
RUN npm run build

FROM golang:1.25-alpine AS build
WORKDIR /src/authman
COPY limbgo /src/limbgo
COPY authman/go.mod ./
COPY authman/go.sum ./
COPY authman/core ./core
RUN go build -trimpath -ldflags="-s -w" -o /out/authman ./core/cmd/authman

FROM alpine:3.21
RUN adduser -D -H -u 10001 authman
COPY --from=build /out/authman /usr/local/bin/authman
COPY --from=web-build /src/authman/core/web/dist /usr/share/authman/core-web
ENV AUTHMAN_CORE_WEB_ROOT=/usr/share/authman/core-web
USER authman
EXPOSE 8080 8081
ENTRYPOINT ["/usr/local/bin/authman"]
