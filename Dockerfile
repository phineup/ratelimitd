FROM golang:1.24-alpine AS build
WORKDIR /src
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ratelimitd ./cmd/ratelimitd

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ratelimitd /ratelimitd
COPY config.example.json /etc/ratelimitd/config.json
EXPOSE 8080
ENTRYPOINT ["/ratelimitd"]
CMD ["-config", "/etc/ratelimitd/config.json"]
