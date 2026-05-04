FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/moltenhub-blank ./cmd/moltenhub-blank

FROM gcr.io/distroless/static-debian12
WORKDIR /workspace
VOLUME ["/workspace/config"]
EXPOSE 8080
ENV APP_DATA_DIR=/workspace/config
COPY --from=build /out/moltenhub-blank /usr/local/bin/moltenhub-blank
ENTRYPOINT ["/usr/local/bin/moltenhub-blank"]
