FROM golang:1.25-alpine AS build
WORKDIR /src/authman
COPY limbgo /src/limbgo
COPY authman/go.mod ./
COPY authman/go.sum ./
COPY authman/cmd ./cmd
COPY authman/internal ./internal
RUN go build -trimpath -ldflags="-s -w" -o /out/authman ./cmd/authman

FROM alpine:3.21
RUN adduser -D -H -u 10001 authman
COPY --from=build /out/authman /usr/local/bin/authman
USER authman
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/authman"]
