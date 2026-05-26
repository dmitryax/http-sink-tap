FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/http-sink-tap .

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/http-sink-tap /http-sink-tap
EXPOSE 8080 8081
USER nonroot:nonroot
ENTRYPOINT ["/http-sink-tap"]
