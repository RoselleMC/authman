FROM golang:1.25-alpine AS build
ARG GOPROXY=https://proxy.golang.org,direct
ENV GOPROXY=${GOPROXY}
WORKDIR /src
COPY limbgo ./limbgo
COPY authman ./authman
WORKDIR /src/authman
RUN go build -trimpath -ldflags="-s -w" -o /out/authman-limbo ./limbo/cmd/authman-limbo

FROM alpine:3.21
RUN adduser -D -H -u 10001 authman
COPY --from=build /out/authman-limbo /usr/local/bin/authman-limbo
USER authman
EXPOSE 25565
ENTRYPOINT ["/usr/local/bin/authman-limbo"]
