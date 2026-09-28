FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY main.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /mc-stats .

FROM alpine
COPY --from=build /mc-stats /usr/local/bin/mc-stats
USER 1000:1000
EXPOSE 9225
ENTRYPOINT ["mc-stats"]
