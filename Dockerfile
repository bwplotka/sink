FROM golang:1.21-bullseye AS buildbase
WORKDIR /app
COPY . ./

FROM buildbase as appbase
RUN CGO_ENABLED=0 go build -mod=vendor -o prw2gcm cmd/prw2gcm/*.go

FROM gcr.io/distroless/static-debian11:latest
COPY --from=appbase /app/prw2gcm /bin/prw2gcm
ENTRYPOINT ["/bin/prw2gcm"]
