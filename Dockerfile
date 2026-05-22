FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum* ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o /firebat-ec ./cmd/firebat-ec

FROM scratch
COPY --from=build /firebat-ec /firebat-ec
USER 0:0
ENTRYPOINT ["/firebat-ec"]
CMD ["daemon"]
CMD ["daemon"]
