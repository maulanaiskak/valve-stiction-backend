# Multi-stage: build the React frontend, then the Go backend, then ship
# just the backend binary + frontend's static build output -- one image,
# matching "go service untuk serve fe" (see docs/V3_PLAN.md).
FROM node:20-slim AS frontend-build
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ .
RUN npm run build

FROM golang:1.25 AS backend-build
WORKDIR /app
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ .
RUN CGO_ENABLED=0 go build -o /backend .

FROM gcr.io/distroless/static-debian12
WORKDIR /app
COPY --from=backend-build /backend .
COPY --from=frontend-build /app/dist ./static
ENV STATIC_DIR=/app/static
EXPOSE 8080
ENTRYPOINT ["/app/backend"]
