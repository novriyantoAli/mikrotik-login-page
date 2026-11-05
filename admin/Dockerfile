# Builds the Admin Panel once, then serves the static files with nginx. The panel
# is its own artefact on the same origin (ADR 0028): its own build, its own image
# tag, its own static root. No Node process runs in production and no server-side
# render holds an admin session.
FROM node:22-alpine AS build

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY . .
RUN npm run build

FROM nginx:1.27-alpine AS runner

COPY docker/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=build /app/build /usr/share/nginx/html

EXPOSE 80
