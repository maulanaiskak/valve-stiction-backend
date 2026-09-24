FROM golang:1.25 AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /backend .

FROM gcr.io/distroless/static-debian12
COPY --from=build /backend /backend
EXPOSE 8080
ENTRYPOINT ["/backend"]
