FROM golang:1.27-alpine AS build
WORKDIR /src
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/ratelimitd ./cmd/ratelimitd

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ratelimitd /ratelimitd
COPY config.example.json /etc/ratelimitd/config.json
EXPOSE 8080
ENTRYPOINT ["/ratelimitd"]
CMD ["-config", "/etc/ratelimitd/config.json"]
